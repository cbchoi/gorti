// Package fom is the top-level for FOM parsing, validation, and the immutable
// FOM model.
//
// Subpackages ( owns):
//   - parser: 1516-2010 DIF XML parser + numbered diagnostic codes.
//   - mim:    embedded standard MIM + HLAstandardMIM, merge with user modules.
//   - model:  immutable FOM data structures.
//
// This package MUST NOT depend on rti/internal/* — it is a standalone library.
// See docs/idd.md §3 for the dependency rules.
package fom
