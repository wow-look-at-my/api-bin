# api-bin

A declarative XML language for binary layouts, and the generator that turns a spec into Go reader code. It is the binary-file sibling of api-cli, on the same api-dsl XML layer.

- `binspec/spec.go` parses a spec into `Spec`. `binspec/gen.go` renders `binspec/reader.go.tmpl` and gofmts the result.
- `cmd_gen.go` is the `api-bin gen` command. Each command lives in its own file and registers itself in `init()`.
- The language carries no vocabulary of a consumer. A spec binds its own types, version streams and flags to Go expressions in its header.
- `testdata/*.xml` each have a `.golden` partner: the exact generated file. A spec with no partner fails the suite.
- Build and test with `go-toolchain` in the repository root.
