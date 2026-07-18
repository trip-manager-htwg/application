package database

import "embed"

//go:embed migrations/*
var EmbeddedMigrations embed.FS
