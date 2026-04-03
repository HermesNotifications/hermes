-- Better Auth core tables (user, session, account, verification).
-- These are normally auto-created by Better Auth on first startup, but we
-- manage them here so the seed command can insert the dev admin user before
-- the portal has ever been started.

CREATE TABLE IF NOT EXISTS "user" (
    id              TEXT PRIMARY KEY,
    name            TEXT NOT NULL,
    email           TEXT NOT NULL UNIQUE,
    "emailVerified" BOOLEAN NOT NULL DEFAULT false,
    image           TEXT,
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT now(),
    "updatedAt"     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS session (
    id              TEXT PRIMARY KEY,
    "userId"        TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    "expiresAt"     TIMESTAMPTZ NOT NULL,
    token           TEXT NOT NULL UNIQUE,
    "ipAddress"     TEXT,
    "userAgent"     TEXT,
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT now(),
    "updatedAt"     TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS account (
    id                         TEXT PRIMARY KEY,
    "providerId"               TEXT NOT NULL,
    "accountId"                TEXT NOT NULL,
    "userId"                   TEXT NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    "accessToken"              TEXT,
    "refreshToken"             TEXT,
    "idToken"                  TEXT,
    "accessTokenExpiresAt"     TIMESTAMPTZ,
    "refreshTokenExpiresAt"    TIMESTAMPTZ,
    scope                      TEXT,
    password                   TEXT,
    "createdAt"                TIMESTAMPTZ NOT NULL DEFAULT now(),
    "updatedAt"                TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS verification (
    id              TEXT PRIMARY KEY,
    value           TEXT NOT NULL,
    "expiresAt"     TIMESTAMPTZ NOT NULL,
    identifier      TEXT NOT NULL,
    "createdAt"     TIMESTAMPTZ NOT NULL DEFAULT now(),
    "updatedAt"     TIMESTAMPTZ NOT NULL DEFAULT now()
);
