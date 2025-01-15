-- Database sqlok
CREATE DATABASE sqlok WITH
    OWNER = sqlok
    TEMPLATE = template0
    ENCODING = 'UTF-8';

GRANT ALL ON DATABASE sqlok TO sqlok;
GRANT ALL ON SCHEMA public TO sqlok;
