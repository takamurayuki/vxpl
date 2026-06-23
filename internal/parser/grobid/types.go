package grobid

import "encoding/xml"

// tei mirrors the subset of GROBID TEI/XML we care about.
// Field tags map Go fields to XML elements; nested structs mirror nesting;
// slices capture repeating elements (authors, sections, references).
//
// Note on namespaces: GROBID emits xmlns="http://www.tei-c.org/ns/1.0".
// We match local element names and ignore the namespace URI by using
// `xml:"title"` (local name) rather than a namespace-qualified tag.
type tei struct {
    XMLName xml.Name `xml:"TEI"`
    Header  header   `xml:"teiHeader"`
    Text    text     `xml:"text"`
}

type header struct {
    FileDesc fileDesc `xml:"fileDesc"`
}

type fileDesc struct {
    // Title under titleStmt is the cleanest title GROBID produces.
    Title      string     `xml:"titleStmt>title"`
    SourceDesc sourceDesc `xml:"sourceDesc"`
}

type sourceDesc struct {
    // This biblStruct describes the paper itself (authors, DOI live here).
    Bibl biblStruct `xml:"biblStruct"`
}

// biblStruct is reused for BOTH the paper's own metadata (under sourceDesc)
// and each reference (under back/listBibl). Same shape, different location.
type biblStruct struct {
    Authors []author `xml:"analytic>author"`
    Title   string   `xml:"analytic>title"`
    IDs     []idno   `xml:"analytic>idno"`
}

type author struct {
    Forename string `xml:"persName>forename"`
    Surname  string `xml:"persName>surname"`
}

// idno carries a typed identifier: <idno type="DOI">...</idno>, ORCID, MD5, etc.
type idno struct {
    Type  string `xml:"type,attr"`
    Value string `xml:",chardata"`
}

type text struct {
    Front front `xml:"front"`
    Body  body  `xml:"body"`
    Back  back  `xml:"back"`
}

type front struct {
    // Abstract is a block of paragraphs; join them at conversion time.
    AbstractParas []string `xml:"abstract>div>p"`
}

type body struct {
    Divs []div `xml:"div"`
}

// div is one section: a heading plus its paragraphs.
type div struct {
    Head  string   `xml:"head"`
    Paras []string `xml:"p"`
}

type back struct {
    // Each reference in the bibliography is a biblStruct.
    References []biblStruct `xml:"div>listBibl>biblStruct"`
}