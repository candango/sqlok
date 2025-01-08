package sqlok

type Table struct {
	Name   string
	Schema string
	Fields []Field
}

type Field struct {
	Name string
	Type string
}
