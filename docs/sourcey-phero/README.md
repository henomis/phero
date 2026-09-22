# Phero Sourcey API reference

This directory contains the reproducible Sourcey configuration for Phero's Go
API reference. Sourcey reads the repository's real Go module and writes the
generated static site into `web/docs/api`, which Phero's existing GitHub Pages
workflow publishes with the rest of the project documentation.

```bash
cd docs/sourcey-phero
npm ci
npm run build
```

The build is pinned to Sourcey 3.6.5 and reads the repository's pinned Go
module directly. It documents exported APIs from Phero packages while
excluding examples, internal packages, and integration-test infrastructure
from the public navigation.
