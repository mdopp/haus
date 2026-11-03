package laib

import (
	"context"
	"time"
)

// AufgabenFresh is how long a person's task list is kept (haus: /mehr took
// seconds): Laib's aufgabe verb takes its MCP lock alone, so a list asked
// while another tool runs waits for it. The list changes seldom; Homes'
// own ticks forget it at once.
const AufgabenFresh = time.Minute

// aufgabenRefresh is how long one refresh may take, whoever asked.
const aufgabenRefresh = 20 * time.Second

type aufgabenKept struct {
	list Aufgaben
	have bool
	at   time.Time
	err  error         // the last refresh's, while there is no list
	done chan struct{} // set while a refresh runs, closed when it ends
}

// KeptAufgaben is MeineAufgaben, kept AufgabenFresh per person. A caller
// with a list in hand gets it at once, a refresh running behind it when it
// is older; one without waits for the refresh, at most until its own
// context ends, and then has nothing yet (ok false, err the refresh's once
// it failed). The refresh is the cache's, not the caller's: a page's short
// wait does not end it, and the next visit gets its result.
func (c *Client) KeptAufgaben(ctx context.Context, von string) (list Aufgaben, ok bool, err error) {
	c.aufgabenMu.Lock()
	if c.aufgaben == nil {
		c.aufgaben = map[string]*aufgabenKept{}
	}
	k := c.aufgaben[von]
	if k == nil {
		k = &aufgabenKept{}
		c.aufgaben[von] = k
	}
	if k.done == nil && (!k.have || time.Since(k.at) >= AufgabenFresh) {
		k.done = make(chan struct{})
		go c.refreshAufgaben(context.WithoutCancel(ctx), von, k, k.done)
	}
	done := k.done
	if k.have || done == nil {
		list, ok, err = k.list, k.have, k.err
		c.aufgabenMu.Unlock()
		return list, ok, err
	}
	c.aufgabenMu.Unlock()
	select {
	case <-done:
	case <-ctx.Done():
	}
	c.aufgabenMu.Lock()
	defer c.aufgabenMu.Unlock()
	if !k.have && k.err == nil {
		return k.list, false, ctx.Err()
	}
	return k.list, k.have, k.err
}

func (c *Client) refreshAufgaben(ctx context.Context, von string, k *aufgabenKept, done chan struct{}) {
	defer close(done)
	ctx, cancel := context.WithTimeout(ctx, aufgabenRefresh)
	defer cancel()
	list, err := c.meineAufgaben(ctx, von)
	c.aufgabenMu.Lock()
	defer c.aufgabenMu.Unlock()
	k.done = nil
	if err != nil {
		if !k.have {
			k.err = err
		}
		return // a failed refresh keeps the last list; the next visit asks again
	}
	if c.aufgaben[von] == k { // not forgotten meanwhile
		k.list, k.have, k.at, k.err = list, true, time.Now(), nil
	}
}

// keepAufgaben puts a list just read in the cache.
func (c *Client) keepAufgaben(von string, list Aufgaben) {
	c.aufgabenMu.Lock()
	defer c.aufgabenMu.Unlock()
	if c.aufgaben == nil {
		c.aufgaben = map[string]*aufgabenKept{}
	}
	k := c.aufgaben[von]
	if k == nil {
		k = &aufgabenKept{}
		c.aufgaben[von] = k
	}
	k.list, k.have, k.at, k.err = list, true, time.Now(), nil
}

// forgetAufgaben drops every kept list: a task ticked or deferred through
// Homes may be anyone's.
func (c *Client) forgetAufgaben() {
	c.aufgabenMu.Lock()
	defer c.aufgabenMu.Unlock()
	c.aufgaben = nil
}
