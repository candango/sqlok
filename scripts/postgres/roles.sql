DROP ROLE IF EXISTS sqlok;

CREATE ROLE sqlok LOGIN
    PASSWORD 'PGSQL_SQLOK_PASSWORD'
    NOSUPERUSER INHERIT NOCREATEDB NOCREATEROLE;
