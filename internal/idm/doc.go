// Package idm defines the intermediate document model that sits between PDF
// extraction and EPUB assembly.
//
// The model is deliberately dumb: it holds values and knows how to check them,
// nothing more. Every pipeline stage takes an idm.Document (or a slice from it),
// returns a transformed copy, and can therefore be tested by comparing values
// rather than by inspecting a PDF.
//
// The model is JSON-serialisable on purpose. An optional out-of-process layout
// stage (see PROPOSAL.md §10.2) can receive and return it without a shared
// library or a shared language.
package idm
