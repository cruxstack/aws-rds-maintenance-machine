# RDS Maintenance Machine UI

React frontend for the RDS Maintenance Machine. Provides a web interface for
managing RDS cluster maintenance operations.

## Production

The UI is built as static assets and embedded directly into the Go server binary
using `go:embed`. When you run `make build-server`, the build process:

1. Builds the React app (`npm run build` in this directory)
2. Copies the output from `dist/` to `internal/app/ui/dist/`
3. Compiles the Go binary with the UI assets embedded

The Go server then serves these static files directly - no separate web server,
CDN, or Node.js runtime is required in production.

## Development

Install dependencies:

```bash
npm install
```

Start the dev server:

```bash
npm run dev
```

### API Backend Configuration

The dev server proxies `/api` and `/mock` requests to a backend server. By
default it connects to the demo server on port 8080.

**Using the demo server** (default):

```bash
npm run dev
```

**Using a local live API server**:

```bash
VITE_API_URL=http://localhost:3010 npm run dev
```

## Build

```bash
npm run build
```

Output is written to `dist/`.
