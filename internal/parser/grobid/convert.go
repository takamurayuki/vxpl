package grobid

import (
    "strings"

    "github.com/takamurayuki/vxpl/internal/parser"
)

// toDocument converts parsed GROBID TEI into a parser.Document.
// It is pure (no I/O), which makes it straightforward to unit-test.
func toDocument(t tei, contentHash string) *parser.Document {
    doc := &parser.Document{
        Schema:     parser.SchemaVersion,
        Engine:     "grobid",
        SourceHash: contentHash,
        Metadata:   buildMetadata(t),
        Sections:   buildSections(t),
        References: buildReferences(t),
    }
    doc.Markdown = renderMarkdown(doc.Sections)
    return doc
}

func buildMetadata(t tei) parser.Metadata {
    bibl := t.Header.FileDesc.SourceDesc.Bibl
    return parser.Metadata{
        Title:    strings.TrimSpace(t.Header.FileDesc.Title),
        Authors:  authorNames(bibl.Authors),
        DOI:      findDOI(bibl.IDs),
        Abstract: strings.TrimSpace(strings.Join(t.Text.Front.AbstractParas, "\n\n")),
    }
}

// authorNames joins forename + surname into display strings, skipping blanks.
func authorNames(authors []author) []string {
    names := make([]string, 0, len(authors))
    for _, a := range authors {
        name := strings.TrimSpace(strings.TrimSpace(a.Forename) + " " + strings.TrimSpace(a.Surname))
        if name != "" {
            names = append(names, name)
        }
    }
    return names
}

// findDOI scans typed idno entries for a DOI (case-insensitive type match).
func findDOI(ids []idno) string {
    for _, id := range ids {
        if strings.EqualFold(id.Type, "DOI") {
            return strings.TrimSpace(id.Value)
        }
    }
    return ""
}

// buildSections turns each TEI <div> (heading + paragraphs) into a Section.
func buildSections(t tei) []parser.Section {
    sections := make([]parser.Section, 0, len(t.Text.Body.Divs))
    for _, d := range t.Text.Body.Divs {
        paras := make([]string, 0, len(d.Paras))
        for _, p := range d.Paras {
            if s := strings.TrimSpace(p); s != "" {
                paras = append(paras, s)
            }
        }
        head := strings.TrimSpace(d.Head)
        if head == "" && len(paras) == 0 {
            continue // skip empty divs
        }
        sections = append(sections, parser.Section{
            Level:    1,
            Heading:  head,
            Markdown: strings.Join(paras, "\n\n"),
        })
    }
    return sections
}

// buildReferences flattens each bibliography biblStruct into a Reference.
func buildReferences(t tei) []parser.Reference {
    refs := make([]parser.Reference, 0, len(t.Text.Back.References))
    for _, r := range t.Text.Back.References {
        ref := parser.Reference{
            Title:   strings.TrimSpace(r.Title),
            Authors: authorNames(r.Authors),
            DOI:     findDOI(r.IDs),
        }
        ref.Raw = rawReference(ref)
        refs = append(refs, ref)
    }
    return refs
}

// rawReference builds a human-readable citation string from the parts we have.
func rawReference(r parser.Reference) string {
    parts := make([]string, 0, 3)
    if len(r.Authors) > 0 {
        parts = append(parts, strings.Join(r.Authors, ", "))
    }
    if r.Title != "" {
        parts = append(parts, r.Title)
    }
    if r.DOI != "" {
        parts = append(parts, "DOI: "+r.DOI)
    }
    return strings.Join(parts, ". ")
}

// renderMarkdown flattens sections into a single Markdown body for direct LLM use.
func renderMarkdown(sections []parser.Section) string {
    var b strings.Builder
    for _, s := range sections {
        if s.Heading != "" {
            b.WriteString("## ")
            b.WriteString(s.Heading)
            b.WriteString("\n\n")
        }
        if s.Markdown != "" {
            b.WriteString(s.Markdown)
            b.WriteString("\n\n")
        }
    }
    return strings.TrimSpace(b.String())
}