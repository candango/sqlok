package model

type AuserBase struct {
	Id          int    `sqlok:"primary_key"`
	Name        string `sqlok:"max_lenght=255"`
	Description string `sqlok:"text"`
}
