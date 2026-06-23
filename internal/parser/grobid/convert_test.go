package grobid

import (
    "encoding/xml"
    "testing"
)

// A compact but representative TEI sample: title, two authors (one with a
// blank forename), a DOI mixed in with other idno types, an abstract, two
// body sections (one empty div that must be skipped), and one reference.
const sampleTEI = `<?xml version="1.0" encoding="UTF-8"?>
<TEI xmlns="http://www.tei-c.org/ns/1.0">
  <teiHeader>
    <fileDesc>
      <titleStmt><title level="a" type="main">  A Study of Things  </title></titleStmt>
      <sourceDesc>
        <biblStruct>
          <analytic>
            <author><persName><forename type="first">Tak-Pong</forename><surname>Woo</surname></persName></author>
            <author><persName><surname>Smith</surname></persName></author>
            <idno type="ORCID">0009-0000-4728-6868</idno>
            <idno type="DOI">10.1234/abcd.5678</idno>
            <title level="a" type="main">A Study of Things</title>
          </analytic>
        </biblStruct>
      </sourceDesc>
    </fileDesc>
  </teiHeader>
  <text>
    <front>
      <abstract>
        <div><p>First abstract paragraph.</p><p>Second one.</p></div>
      </abstract>
    </front>
    <body>
      <div><head>Introduction</head><p>Intro para one.</p><p>Intro para two.</p></div>
      <div><p>   </p></div>
      <div><head>Methods</head><p>We did things.</p></div>
    </body>
    <back>
      <div>
        <listBibl>
          <biblStruct>
            <analytic>
              <author><persName><forename>Jane</forename><surname>Doe</surname></persName></author>
              <idno type="DOI">10.9999/ref.0001</idno>
              <title level="a" type="main">A Cited Work</title>
            </analytic>
          </biblStruct>
        </listBibl>
      </div>
    </back>
  </text>
</TEI>`

// parseSample unmarshals the fixture into the tei struct for the tests.
func parseSample(t *testing.T) tei {
    t.Helper()
    var doc tei
    if err := xml.Unmarshal([]byte(sampleTEI), &doc); err != nil {
        t.Fatalf("failed to unmarshal sample TEI: %v", err)
    }
    return doc
}

func TestToDocument_Metadata(t *testing.T) {
    doc := toDocument(parseSample(t), "hash123")

    if doc.Schema != "1.0" {
        t.Errorf("Schema = %q, want %q", doc.Schema, "1.0")
    }
    if doc.Engine != "grobid" {
        t.Errorf("Engine = %q, want %q", doc.Engine, "grobid")
    }
    if doc.SourceHash != "hash123" {
        t.Errorf("SourceHash = %q, want %q", doc.SourceHash, "hash123")
    }
    if doc.Metadata.Title != "A Study of Things" {
        t.Errorf("Title = %q, want trimmed %q", doc.Metadata.Title, "A Study of Things")
    }
    if doc.Metadata.DOI != "10.1234/abcd.5678" {
        t.Errorf("DOI = %q, want %q", doc.Metadata.DOI, "10.1234/abcd.5678")
    }
    if doc.Metadata.Abstract != "First abstract paragraph.\n\nSecond one." {
        t.Errorf("Abstract = %q", doc.Metadata.Abstract)
    }
}

func TestToDocument_Authors(t *testing.T) {
    doc := toDocument(parseSample(t), "h")

    want := []string{"Tak-Pong Woo", "Smith"}
    if len(doc.Metadata.Authors) != len(want) {
        t.Fatalf("got %d authors %v, want %d", len(doc.Metadata.Authors), doc.Metadata.Authors, len(want))
    }
    for i := range want {
        if doc.Metadata.Authors[i] != want[i] {
            t.Errorf("Authors[%d] = %q, want %q", i, doc.Metadata.Authors[i], want[i])
        }
    }
}

func TestToDocument_SectionsSkipEmpty(t *testing.T) {
    doc := toDocument(parseSample(t), "h")

    // The blank middle div must be dropped, leaving Introduction and Methods.
    if len(doc.Sections) != 2 {
        t.Fatalf("got %d sections, want 2: %+v", len(doc.Sections), doc.Sections)
    }
    if doc.Sections[0].Heading != "Introduction" {
        t.Errorf("Sections[0].Heading = %q, want %q", doc.Sections[0].Heading, "Introduction")
    }
    if doc.Sections[0].Markdown != "Intro para one.\n\nIntro para two." {
        t.Errorf("Sections[0].Markdown = %q", doc.Sections[0].Markdown)
    }
    if doc.Sections[1].Heading != "Methods" {
        t.Errorf("Sections[1].Heading = %q, want %q", doc.Sections[1].Heading, "Methods")
    }
}

func TestToDocument_References(t *testing.T) {
    doc := toDocument(parseSample(t), "h")

    if len(doc.References) != 1 {
        t.Fatalf("got %d references, want 1", len(doc.References))
    }
    ref := doc.References[0]
    if ref.Title != "A Cited Work" {
        t.Errorf("ref.Title = %q, want %q", ref.Title, "A Cited Work")
    }
    if ref.DOI != "10.9999/ref.0001" {
        t.Errorf("ref.DOI = %q, want %q", ref.DOI, "10.9999/ref.0001")
    }
    if len(ref.Authors) != 1 || ref.Authors[0] != "Jane Doe" {
        t.Errorf("ref.Authors = %v, want [Jane Doe]", ref.Authors)
    }
    if ref.Raw != "Jane Doe. A Cited Work. DOI: 10.9999/ref.0001" {
        t.Errorf("ref.Raw = %q", ref.Raw)
    }
}

func TestRenderMarkdown(t *testing.T) {
    doc := toDocument(parseSample(t), "h")

    want := "## Introduction\n\nIntro para one.\n\nIntro para two.\n\n## Methods\n\nWe did things."
    if doc.Markdown != want {
        t.Errorf("Markdown =\n%q\nwant\n%q", doc.Markdown, want)
    }
}