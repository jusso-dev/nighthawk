# SIEM export

Wedgetail's exact API was not found. The `nighthawk.event.v1` envelope in
`internal/siem` is our field map, not a vendor schema. There are no two-way
actions: Nighthawk only forwards events (syslog RFC 5424 shaped lines or NDJSON
webhook POSTs). Operators map fields into their SIEM themselves.
