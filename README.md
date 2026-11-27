# api-bin

A declarative language for binary file layouts. You describe a layout in XML, and `api-bin gen` writes the Go code that reads it. It is the binary-file sibling of [api-cli](https://github.com/wow-look-at-my/api-cli), built on the same [api-dsl](https://github.com/wow-look-at-my/api-dsl) XML layer.

## Build

```sh
go-toolchain
```

## Use

```sh
api-bin gen layout.xml -o layout_gen.go
```

## A spec

A spec opens with its bindings: the Go package to generate into, the wire types, the version streams and the flags. The layouts follow.

```xml
<?xml version="1.1" encoding="UTF-8"?>
<api-bin>
	<go package="main" params="r *Reader, hdr *Header"/>
	<type name="i32" read="r.ReadInt32({label})"/>
	<type name="i64" go="int64" read="r.ReadInt64({label})"/>
	<version stream="file" test="hdr.Version >= {name}"/>
	<flag name="cooked" test="hdr.Cooked"/>

	<struct name="Entry" func="readEntry" label="Entry">
		<field name="Index" type="i32"/>
		<field name="Size" type="i64" since="file:11"/>
		<field name="Size" type="i32" as="i64" label="Size32" until="file:11"/>
		<skip type="i32" label="Padding" unless="cooked"/>
	</struct>
</api-bin>
```

- `<field>` reads a value into the struct member of that name. `<skip>` reads and discards one. `<group>` puts several steps under one condition.
- `since` and `until` test a version stream. `if` and `unless` test a flag. Several on one element join with "and".
- `as` converts the value to another type's Go type. `label` overrides the name in the reader's trace label.
- `<enum>` generates a value-to-name function from its `<value>` rows. Its `unknown` attribute formats a value the table lacks.
- A `<struct>` takes `label` for a fixed label prefix, or `label-expr` for a Go expression such as a parameter. `params` overrides the parameter list of `<go>` for that reader.
- `<call go="...">` puts a hand-written Go statement between the steps, for a part of a layout that the language cannot say. `{label}` is the struct's label prefix and `{out}` is the value under construction.

The language has no built-in types. Each `<type>` names the Go expression that reads it, so a spec targets whatever reader its consumer already has.

[`testdata/uasset_package_tables.xml`](./testdata/uasset_package_tables.xml) is a real spec: the import and export table entries of an Unreal Engine package.

## License

MIT.
