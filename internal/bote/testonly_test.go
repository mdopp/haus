package bote

import "context"

// Only the tests call these: production code calls what they wrap. They
// were in the package itself, where `deadcode` counted them as
// production code nobody runs (scripts/ratchet.py, test-only).

// Ask sends text as p and returns the Bote's reply. Anything but a chat
// completion with text is an error; Hermes' words stay out of it.
func (c *Client) Ask(ctx context.Context, p Person, text string) (string, error) {
	resp, err := c.request(ctx, p, text, false)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	return completion(resp)
}
