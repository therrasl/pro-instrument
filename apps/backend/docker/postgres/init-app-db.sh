#!/bin/sh
set -eu

psql \
  --set=ON_ERROR_STOP=1 \
  --username "$POSTGRES_USER" \
  --dbname "$POSTGRES_DB" \
  --set=app_db="$APP_DB_NAME" \
  --set=app_user="$APP_DB_USER" \
  --set=app_password="$APP_DB_PASSWORD" <<'SQL'
CREATE ROLE :"app_user"
  LOGIN
  PASSWORD :'app_password'
  NOSUPERUSER
  NOCREATEDB
  NOCREATEROLE
  NOREPLICATION;
CREATE DATABASE :"app_db" OWNER :"app_user";
SQL
