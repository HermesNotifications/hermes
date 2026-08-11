// Copyright Hermes Notifications
// SPDX-License-Identifier: Apache-2.0
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { App } from "./App.js";
import "./styles.css";

const root = document.getElementById("root");
if (!root) throw new Error("#root is missing from index.html");

// StrictMode on purpose: it double-invokes effects and state initializers, which is exactly the
// pressure that surfaced the SDK's old habit of constructing a client in a render body.
createRoot(root).render(
  <StrictMode>
    <App />
  </StrictMode>
);
