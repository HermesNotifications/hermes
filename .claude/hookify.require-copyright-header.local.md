---
name: require-copyright-header
enabled: true
event: file
conditions:
  - field: file_path
    operator: regex_match
    pattern: \.(go|ts|tsx|js|jsx|sh)$
  - field: file_path
    operator: not_contains
    pattern: node_modules
  - field: file_path
    operator: not_contains
    pattern: .d.ts
  - field: file_path
    operator: not_contains
    pattern: vendor/
  - field: new_text
    operator: not_contains
    pattern: Copyright 2026 Hermes Notifications
---

**Missing copyright header.** Every source file must begin with the standard two-line header before the package declaration (Go) or at the top of the file (all others).

**Go / TS / JS / JSX / TSX:**
```
// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.
```

**Shell (.sh):**
```
# Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
# See LICENSE and NOTICE in the project root for full terms and restrictions.
```

Add the header now, before completing the write. For Go files, the header goes before the `package` declaration with a blank line between them. For shell scripts with a shebang, the header goes immediately after the shebang line.

Skip generated files (files with "DO NOT EDIT", "Code generated", or similar in their first 5 lines).
