# ORYX Plugins

Place executable scripts in this directory to add custom health checks.

## Naming

Each file becomes a check named by its basename (without extension):
- `disk-usage.sh` → check named `disk-usage`

## Protocol

- Print a human-readable summary to stdout
- Exit 0 for healthy
- Exit 1 for warning  
- Exit 2 for critical
- Exit 3+ for unknown
