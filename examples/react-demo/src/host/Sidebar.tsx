// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

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
