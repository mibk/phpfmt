// Package naive provides a deliberately minimal PHP parser and pretty-printer.
//
// The parser recognises only two node types---blocks (scopes) and generic
// statements (or list items)---so it does not model PHP’s full grammar.
// The printer can re-emit any valid PHP file (and often even invalid ones),
// but it normalises the output: spacing, line breaks, and other cosmetic trivia
// may differ from the original source, although the program’s behaviour
// is preserved.
package naive
