package main

import (
	"fmt"
	"strings"
)

func deepJSON(depth int) []byte {
	return []byte(strings.Repeat("[", depth) + strings.Repeat("]", depth) + "\n")
}

// yamlAliasBomb nests anchors: each level is a list of fanout aliases of the
// previous one, so expansion is fanout^levels while the file stays tiny.
func yamlAliasBomb(levels, fanout int) []byte {
	var b strings.Builder
	items := make([]string, fanout)
	for i := range items {
		items[i] = `"lol"`
	}
	fmt.Fprintf(&b, "l0: &l0 [%s]\n", strings.Join(items, ","))
	for l := 1; l <= levels; l++ {
		for i := range items {
			items[i] = fmt.Sprintf("*l%d", l-1)
		}
		fmt.Fprintf(&b, "l%d: &l%d [%s]\n", l, l, strings.Join(items, ","))
	}
	return []byte(b.String())
}

// xmlBillionLaughs is the classic nested-entity expansion.
func xmlBillionLaughs(levels, fanout int) []byte {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\"?>\n<!DOCTYPE lolz [\n  <!ENTITY lol0 \"lol\">\n")
	for l := 1; l <= levels; l++ {
		fmt.Fprintf(&b, "  <!ENTITY lol%d \"%s\">\n", l, strings.Repeat(fmt.Sprintf("&lol%d;", l-1), fanout))
	}
	fmt.Fprintf(&b, "]>\n<lolz>&lol%d;</lolz>\n", levels)
	return []byte(b.String())
}

func genStructured(c *corpus) error {
	levels := 9
	if c.large {
		levels = 12
	}
	const xxe = "<?xml version=\"1.0\"?>\n<!DOCTYPE note [\n  <!ENTITY xxe SYSTEM \"file:///etc/passwd\">\n]>\n<note><to>kilnfile</to><body>&xxe;</body></note>\n"
	const pi = "<?xml version=\"1.0\"?>\n<?xml-stylesheet type=\"text/xsl\" href=\"http://example.invalid/s.xsl\"?>\n<note><to>kilnfile</to><?kiln-pi inert-instruction?><body>hello</body></note>\n"
	notDTD := []string{"<!DOCTYPE", "<!ENTITY", "file:///etc/passwd"}
	samples := []struct {
		path, desc string
		data       string
		exp        Expected
	}{
		{"structured/benign.json", "Benign JSON object.", `{"name":"kilnfile","values":[1,2,3],"nested":{"ok":true}}` + "\n", ok},
		{"structured/benign.yaml", "Benign YAML mapping.", "name: kilnfile\nvalues:\n  - 1\n  - 2\nnested:\n  ok: true\n", ok},
		{"structured/benign.xml", "Benign XML document.", "<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<note><to>kilnfile</to><body>hello</body></note>\n", ok},
		{"structured/deep.json", "JSON nested 10000 arrays deep (default max_structured_depth 256).", string(deepJSON(10000)), rejected(rsnLimitExceeded)},
		{"structured/alias_bomb.yaml", fmt.Sprintf("YAML billion laughs: %d anchored levels of 9 aliases each.", levels+1), string(yamlAliasBomb(levels, 9)), rejected(rsnLimitExceeded)},
		{"structured/billion_laughs.xml", fmt.Sprintf("XML billion laughs: %d nested entity levels of 10 references each.", levels+1), string(xmlBillionLaughs(levels, 10)), rejected(rsnLimitExceeded)},
		{"structured/xxe.xml", "XML with a DOCTYPE declaring an external entity to file:///etc/passwd and a reference to it. DTD and entities are dropped.", xxe,
			Expected{Result: resSanitized, MustRemove: []string{actDTD}, MustNotContain: notDTD, MustContain: []string{"<note>"}}},
		{"structured/processing_instruction.xml", "XML with an xml-stylesheet PI to http://example.invalid/s.xsl and a custom PI in the body.", pi,
			Expected{Result: resSanitized, MustRemove: []string{actPI}, MustNotContain: []string{"xml-stylesheet", "example.invalid", "kiln-pi"}, MustContain: []string{"<note>"}}},
		{"structured/malformed.json", "Malformed JSON (double comma, unclosed object).", `{"a": [1, 2,, }` + "\n", rejected(rsnUnparseable)},
	}
	for _, s := range samples {
		if err := c.add(s.path, "structured", s.desc, []byte(s.data), s.exp); err != nil {
			return err
		}
	}
	return nil
}
