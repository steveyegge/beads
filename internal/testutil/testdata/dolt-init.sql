-- Workaround for Dolt Docker image auth bug: the default root user is
-- created as root@localhost, but the go-sql-driver connects as root@%
-- (via mapped TCP port), which fails authentication on Dolt >= 1.44.
-- Not currently used — the Dolt image doesn't process /docker-entrypoint-initdb.d/.
-- Kept for when DoltHub adds init script support to their Docker image.
CREATE USER IF NOT EXISTS 'root'@'%' IDENTIFIED BY '';
GRANT ALL ON *.* TO 'root'@'%' WITH GRANT OPTION;
