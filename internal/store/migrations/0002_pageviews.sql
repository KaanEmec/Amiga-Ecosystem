-- First-party, cookie-free page views. visitor is a daily-rotating hash
-- (secret, UTC day, client IP, User-Agent); nothing identifying is stored.
-- day is the UTC date (yyyy-mm-dd) of at. Rows older than 400 days are pruned.
CREATE TABLE pageviews (
    id       INTEGER PRIMARY KEY,
    at       INTEGER NOT NULL,
    day      TEXT    NOT NULL,
    path     TEXT    NOT NULL,
    visitor  TEXT    NOT NULL,
    referrer TEXT    NOT NULL DEFAULT ''
) STRICT;

CREATE INDEX pageviews_day ON pageviews (day);
CREATE INDEX pageviews_visitor_at ON pageviews (visitor, at);
