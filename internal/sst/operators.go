package sst

type ComparisonOperator string

const (
	Equal              ComparisonOperator = "="
	NotEqual           ComparisonOperator = "<>"
	GreaterThan        ComparisonOperator = ">"
	GreaterThanOrEqual ComparisonOperator = ">="
	LessThan           ComparisonOperator = "<"
	LessThanOrEqual    ComparisonOperator = "<="
)

type MembershipOperator uint8

const (
	In MembershipOperator = iota
	NotIn
)

type NullOperator uint8

const (
	IsNull NullOperator = iota
	IsNotNull
)

type RangeOperator uint8

const (
	Between RangeOperator = iota
	NotBetween
)

type PatternOperator uint8

const (
	Like PatternOperator = iota
	NotLike
	// ILike e NoILike go to dialects
)

// BooleanOperator identifies a logical SQL operator.
type BooleanOperator string

const (
	AndOperator BooleanOperator = "AND"
	OrOperator  BooleanOperator = "OR"
	NotOperator BooleanOperator = "NOT"
)

// TODO: those should be categorized correctly
// The OtherOperators is just temporary
type OtherOperators uint8

const (
	Exists OtherOperators = iota
	NotExists
	IsDistinctFrom
	IsNotDistinctFrom
)
