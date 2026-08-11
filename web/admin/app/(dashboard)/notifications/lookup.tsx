// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE in the project root for license terms and DISCLAIMER.md for important usage information.

"use client";

import * as React from "react";
import { useRouter } from "next/navigation";
import { SearchIcon } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";

export function NotificationLookup() {
  const [id, setId] = React.useState("");
  const router = useRouter();

  function handleSubmit(e: React.FormEvent) {
    e.preventDefault();
    const trimmed = id.trim();
    if (!trimmed) return;
    router.push(`/notifications/${trimmed}`);
  }

  return (
    <form onSubmit={handleSubmit} className="flex gap-2">
      <Input
        placeholder="Notification ID"
        value={id}
        onChange={(e) => setId(e.target.value)}
        className="max-w-sm"
      />
      <Button type="submit" disabled={!id.trim()}>
        <SearchIcon />
        Look up
      </Button>
    </form>
  );
}
