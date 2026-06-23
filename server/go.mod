module github.com/catalystcommunity/piler/server

go 1.26.3

require (
	github.com/catalystcommunity/csilgen/transports/go v0.0.0-20260619204013-714fbcee2486
	github.com/catalystcommunity/piler/coredb v0.0.0-00010101000000-000000000000
	github.com/catalystcommunity/websocks v0.0.0-00010101000000-000000000000
	github.com/fxamacker/cbor/v2 v2.9.2
	github.com/google/uuid v1.6.0
	github.com/jackc/pgx/v5 v5.7.6
	github.com/pressly/goose/v3 v3.24.1
)

require (
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.2 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/sethvargo/go-retry v0.3.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/crypto v0.37.0 // indirect
	golang.org/x/sync v0.13.0 // indirect
	golang.org/x/text v0.24.0 // indirect
)

replace github.com/catalystcommunity/piler/coredb => ../coredb

replace github.com/catalystcommunity/websocks => ../../websocks
