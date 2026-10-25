package judge

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Bild is one photo: its bytes and its type (image/jpeg …).
type Bild struct {
	Data []byte
	Type string
}

// Plattenhinweise is what photos of a record say (Platte erfassen, owner
// 2026-10-25): the barcode's digits as printed under it, the catalogue
// number, the label, the artist and the title — each "" when not seen.
type Plattenhinweise struct {
	Barcode       string `json:"barcode"`
	Katalognummer string `json:"katalognummer"`
	Label         string `json:"label"`
	Kuenstler     string `json:"kuenstler"`
	Titel         string `json:"titel"`
}

// visionModel is the model that reads photos: HAUS_LLM_VISION_MODEL
// (laib#697: the 12B with its image part), else the house model, which
// on this box can.
func (c *Client) visionModel() string {
	if m := strings.TrimSpace(os.Getenv("HAUS_LLM_VISION_MODEL")); m != "" {
		return m
	}
	return c.Model
}

const platteSystem = "Du liest Fotos einer Schallplatte: Vorderseite, Rückseite, das Etikett auf der Platte, den Rücken. " +
	"Schreib nur ab, was wirklich zu lesen ist, nichts geraten. barcode: die Ziffern unter dem Strichcode, ohne Leerzeichen. " +
	"katalognummer: die Bestellnummer des Labels, wie sie gedruckt ist (z. B. „PB 011“, „GHVC 077“). label: der Name des Labels. " +
	"kuenstler und titel: Künstler und Albumtitel. Was nicht zu sehen ist, bleibt leer. Antworte nur mit JSON."

// LiesPlatte asks the vision model what photos of a record say. Only while
// that model is the one in place (ErrNotLoaded otherwise, saying who has
// the card); an answer that is not the schema's is an error.
func (c *Client) LiesPlatte(ctx context.Context, bilder []Bild) (Plattenhinweise, error) {
	model := c.visionModel()
	if err := c.loadedAs(ctx, model); err != nil {
		return Plattenhinweise{}, err
	}
	content := []map[string]any{{"type": "text", "text": "Was steht auf dieser Platte?"}}
	for _, b := range bilder {
		content = append(content, map[string]any{"type": "image_url",
			"image_url": map[string]string{"url": "data:" + b.Type + ";base64," + base64.StdEncoding.EncodeToString(b.Data)}})
	}
	field := map[string]any{"type": "string", "maxLength": 120}
	schema := map[string]any{"type": "object", "additionalProperties": false,
		"required":   []string{"barcode", "katalognummer", "label", "kuenstler", "titel"},
		"properties": map[string]any{"barcode": field, "katalognummer": field, "label": field, "kuenstler": field, "titel": field}}
	body, err := json.Marshal(map[string]any{
		"model": model, "temperature": 0, "max_tokens": 300,
		"messages":             []map[string]any{{"role": "system", "content": platteSystem}, {"role": "user", "content": content}},
		"response_format":      map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "platte", "schema": schema}},
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	})
	if err != nil {
		return Plattenhinweise{}, err
	}
	raw, err := c.do(ctx, http.MethodPost, c.URL+"/chat/completions", c.Key, body)
	if err != nil {
		return Plattenhinweise{}, err
	}
	var ans struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &ans); err != nil || len(ans.Choices) == 0 {
		return Plattenhinweise{}, fmt.Errorf("judge: not a chat completion: %v", err)
	}
	var h Plattenhinweise
	if err := json.Unmarshal([]byte(ans.Choices[0].Message.Content), &h); err != nil {
		return Plattenhinweise{}, fmt.Errorf("judge: the answer is no record: %q", cut(ans.Choices[0].Message.Content))
	}
	return h, nil
}
