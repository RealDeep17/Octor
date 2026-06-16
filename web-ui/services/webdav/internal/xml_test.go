package internal

import (
	"bytes"
	"encoding/xml"
	"io"
	"testing"
)

const rawXML = `<?xml version="1.0" encoding="UTF-8"?>
<bookstore>
  <book category="COOKING">
    <title lang="en">Everyday Italian</title>
    <author>Giada De Laurentiis</author>
    <year>2005</year>
  </book>

  <book category="CHILDREN">
    <title lang="en">Harry Potter</title>
    <author>J K. Rowling</author>
    <year>2005</year>
  </book>
</bookstore>`

func TestRawXMLValue(t *testing.T) {
	// Test XML namespaces
	{
		const namespaceXML = `<custom:prop xmlns:custom="urn:custom-ns">text</custom:prop>`
		var rawValue RawXMLValue
		if err := xml.Unmarshal([]byte(namespaceXML), &rawValue); err != nil {
			t.Fatalf("xml.Unmarshal() for namespaceXML = %v", err)
		}

		name, ok := rawValue.XMLName()
		if !ok {
			t.Fatalf("rawValue.XMLName() failed for namespaceXML")
		}
		if name.Space != "urn:custom-ns" {
			t.Errorf("expected Space to be %q, got %q", "urn:custom-ns", name.Space)
		}
		if name.Local != "prop" {
			t.Errorf("expected Local to be %q, got %q", "prop", name.Local)
		}

		b, err := xml.Marshal(&rawValue)
		if err != nil {
			t.Fatalf("xml.Marshal() for namespaceXML = %v", err)
		}

		var roundTrip RawXMLValue
		if err := xml.Unmarshal(b, &roundTrip); err != nil {
			t.Fatalf("roundtrip xml.Unmarshal() = %v", err)
		}
		nameRT, ok := roundTrip.XMLName()
		if !ok {
			t.Fatalf("roundTrip.XMLName() failed")
		}
		if nameRT.Space != "urn:custom-ns" || nameRT.Local != "prop" {
			t.Errorf("roundtrip namespace/local mismatch: got space=%q, local=%q", nameRT.Space, nameRT.Local)
		}
	}


	var rawValue RawXMLValue
	if err := xml.Unmarshal([]byte(rawXML), &rawValue); err != nil {
		t.Fatalf("xml.Unmarshal() = %v", err)
	}

	b, err := xml.Marshal(&rawValue)
	if err != nil {
		t.Fatalf("xml.Marshal() = %v", err)
	}

	s := xml.Header + string(b)
	if s != rawXML {
		t.Errorf("input doesn't match output:\n%v\nvs.\n%v", rawXML, s)
	}
}

func TestRawXMLValue_TokenReader(t *testing.T) {
	var rawValue RawXMLValue
	if err := xml.Unmarshal([]byte(rawXML), &rawValue); err != nil {
		t.Fatalf("xml.Unmarshal() = %v", err)
	}

	tr := rawValue.TokenReader()

	var buf bytes.Buffer
	enc := xml.NewEncoder(&buf)
	for {
		tok, err := tr.Token()
		if err == io.EOF {
			break
		} else if err != nil {
			t.Fatalf("TokenReader.Token() = %v", err)
		}

		if err := enc.EncodeToken(tok); err != nil {
			t.Fatalf("Encoder.EncodeToken() = %v", err)
		}
	}
	if err := enc.Flush(); err != nil {
		t.Fatalf("Encoder.Flush() = %v", err)
	}

	s := xml.Header + buf.String()
	if s != rawXML {
		t.Errorf("input doesn't match output:\n%v\nvs.\n%v", rawXML, s)
	}
}
