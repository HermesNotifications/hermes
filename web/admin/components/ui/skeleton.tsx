// Copyright 2026 Hermes Notifications. Licensed under the Apache License, Version 2.0.
// See LICENSE and NOTICE in the project root for full terms and restrictions.

import * as React from "react"

import { cn } from "@/lib/utils"

function Skeleton({ className, ...props }: React.ComponentProps<"div">) {
  return (
    <div
      data-slot="skeleton"
      className={cn("animate-pulse rounded-md bg-muted", className)}
      {...props}
    />
  )
}

export { Skeleton }
