package schema

type ReferenceOption string

const (
	ReferenceOptionCascade    ReferenceOption = "CASCADE"
	ReferenceOptionNoAction   ReferenceOption = "NO ACTION"
	ReferenceOptionSetDefault ReferenceOption = "SET DEFAULT"
	ReferenceOptionSetNull    ReferenceOption = "SET NULL"
	ReferenceOptionRestrict   ReferenceOption = "RESTRICT"
)

type Table struct {
	Fields      []*Field
	ForeingKeys []*ForeingKey
	Name        string
	Schema      string
}

type Field struct {
	Default  string
	Name     string
	Nullable bool
	Primary  bool
	Type     string
}

type ForeingKey struct {
	Fields         []*Field
	Name           string
	OnDelete       ReferenceOption
	OnUpdate       ReferenceOption
	ReferredTable  []*Table
	ReferredFields []*Field
}
