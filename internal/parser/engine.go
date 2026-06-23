// Package parser defines the contract between the orchestration layer and the
// concrete document-parsing backends (GROBID, MinerU, Marker, ...).
package parser

import (
    "context"
    "io"
    "time"
)

// SchemaVersion identifies the shape of Document. Bump it whenever the output
// structure changes so downstream consumers (rapitas) can detect and adapt.
const SchemaVersion = "1.0"

// Engine turns a raw document into a structured, AI-friendly representation.
type Engine interface {
    Parse(ctx context.Context, r io.Reader, opts Options) (*Document, error)
    Capabilities() Capabilities
    Name() string
}

// Options carries per-call parameters and hints.
type Options struct {
    ContentHash string
    Languages   []string
    Want        Feature
}

// Feature is a bitset of extractable element types.
type Feature uint16

const (
    FeatureMetadata   Feature = 1 << iota // title, authors, DOI, abstract
    FeatureBody                           // body text with reading order
    FeatureReferences                     // bibliography / citations
    FeatureTables                         // structured tables
    FeatureFormulas                       // math, emitted as LaTeX
    FeatureFigures                        // figures + captions
)

// Has reports whether the set contains x.
func (f Feature) Has(x Feature) bool { return f&x != 0 }

// Capabilities describes what an Engine produces and how heavy it is.
type Capabilities struct {
    Supported Feature
    Profile   ResourceProfile
}

// ResourceProfile is a coarse scheduling hint.
type ResourceProfile int

const (
    ProfileCPU ResourceProfile = iota // light, parallelizable
    ProfileGPU                        // heavy, batch and limit concurrency
)

// Document is the structured, AI-friendly result of a parse.
type Document struct {
    Schema     string    `json:"schema"`
    Engine     string    `json:"engine"`
    SourceHash string    `json:"source_hash"`
    ParsedAt   time.Time `json:"parsed_at"`

    Metadata   Metadata    `json:"metadata"`
    Sections   []Section   `json:"sections"`
    References []Reference `json:"references"`
    Figures    []Figure    `json:"figures,omitempty"`
    Tables     []Table     `json:"tables,omitempty"`

    Markdown string  `json:"markdown"`
    Chunks   []Chunk `json:"chunks,omitempty"`
    Warnings []string `json:"warnings,omitempty"`
}

type Metadata struct {
    Title    string   `json:"title"`
    Authors  []string `json:"authors"`
    DOI      string   `json:"doi,omitempty"`
    Abstract string   `json:"abstract,omitempty"`
    Year     int      `json:"year,omitempty"`
}

type Section struct {
    Level    int       `json:"level"`
    Heading  string    `json:"heading"`
    Markdown string    `json:"markdown"`
    Children []Section `json:"children,omitempty"`
}

type Reference struct {
    Raw     string   `json:"raw"`
    Title   string   `json:"title,omitempty"`
    Authors []string `json:"authors,omitempty"`
    DOI     string   `json:"doi,omitempty"`
}

type Figure struct {
    ID       string `json:"id"`
    Caption  string `json:"caption"`
    AssetKey string `json:"asset_key,omitempty"`
}

type Table struct {
    ID       string `json:"id"`
    Caption  string `json:"caption,omitempty"`
    Markdown string `json:"markdown"`
}

type Chunk struct {
    ID      string `json:"id"`
    Section string `json:"section"`
    Text    string `json:"text"`
    Tokens  int    `json:"tokens"`
}