package schema

type ReferenceOption string

const (
	ReferenceOptionCascade    ReferenceOption = "CASCADE"
	ReferenceOptionNoAction   ReferenceOption = "NO ACTION"
	ReferenceOptionSetDefault ReferenceOption = "SET DEFAULT"
	ReferenceOptionSetNull    ReferenceOption = "SET NULL"
	ReferenceOptionRestrict   ReferenceOption = "RESTRICT"
)

// TODO: Think about that, not sure if that is the right thing to do
func WithPrefix(prefix string, name string) string {
	if prefix == "" {
		return name
	}
	return prefix + "." + name
}

type Aliasable interface {
	Name() string
	As(string) string
}

type Table struct {
	Fields      []*Field
	ForeignKeys []*ForeignKey
	TableName   string
	Schema      string
}

func (t *Table) Name() string {
	if t.Schema == "" || t.Schema == "public" {
		return t.TableName
	}
	return t.Schema + "." + t.TableName
}

func (t *Table) As(alias string) string {
	return t.Name() + " AS " + alias
}

type Field struct {
	Default   string
	FieldName string
	Nullable  bool
	Primary   bool
	Type      string
	Table     *Table
}

func (t *Field) Name() string {
	if t.Table != nil {
		return t.Table.Schema + "." + t.Table.TableName + "." + t.FieldName
	}
	return t.FieldName
}

func (t *Field) As(alias string) string {
	return t.Name() + " AS " + alias
}

type ForeignKey struct {
	Fields         []*Field
	Name           string
	OnDelete       ReferenceOption
	OnUpdate       ReferenceOption
	ReferredTable  *Table
	ReferredFields []*Field
}
