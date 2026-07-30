// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

/** Filler chrome, so the widget is judged inside a plausible application rather than on a blank page. */
export function Sidebar() {
  const links = ["Dashboard", "Reports", "Segments", "Integrations", "Settings"];

  return (
    <aside className="sidebar">
      <div className="sidebar-brand">
        <span className="sidebar-mark" aria-hidden="true">
          A
        </span>
        Acme Analytics
      </div>
      <nav className="sidebar-nav" aria-label="Main">
        {links.map((label) => (
          <a
            key={label}
            className="sidebar-link"
            href="#"
            {...(label === "Dashboard" ? { "aria-current": "page" as const } : {})}
          >
            {label}
          </a>
        ))}
      </nav>
    </aside>
  );
}
