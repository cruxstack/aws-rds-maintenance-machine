# RDS Maintenance Machine UI

React frontend for the RDS Maintenance Machine. Provides a web interface for
managing RDS cluster maintenance operations.

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
