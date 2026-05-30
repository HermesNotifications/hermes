"use client";

import * as React from "react";
import Link from "next/link";
import { usePathname } from "next/navigation";
import type { LucideIcon } from "lucide-react";
import { FileText, FolderTree, KeyRound, Bell, Building2, Users } from "lucide-react";
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  SidebarRail,
} from "@/components/ui/sidebar";
import { NamespaceSwitcher } from "@/components/namespace-switcher";
import { NavUser } from "@/components/nav-user";

interface NavItem {
  title: string;
  href: string;
  icon: LucideIcon;
}

const navGroups: { label: string; items: NavItem[] }[] = [
  {
    label: "Content",
    items: [
      { title: "Templates", href: "/templates", icon: FileText },
      { title: "Subscriptions", href: "/categories", icon: FolderTree },
    ],
  },
  {
    label: "Platform",
    items: [
      { title: "Tenants", href: "/tenants", icon: Building2 },
      { title: "Users", href: "/users", icon: Users },
    ],
  },
  {
    label: "System",
    items: [
      { title: "API Keys", href: "/api-keys", icon: KeyRound },
      { title: "Notifications", href: "/notifications", icon: Bell },
    ],
  },
];

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const pathname = usePathname();

  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarHeader>
        <NamespaceSwitcher />
      </SidebarHeader>
      <SidebarContent>
        {navGroups.map((group) => (
          <SidebarGroup key={group.label}>
            <SidebarGroupLabel>{group.label}</SidebarGroupLabel>
            <SidebarGroupContent>
              <SidebarMenu>
                {group.items.map((item) => (
                  <SidebarMenuItem key={item.href}>
                    <SidebarMenuButton
                      render={<Link href={item.href} />}
                      isActive={pathname.startsWith(item.href)}
                      tooltip={item.title}
                    >
                      <item.icon />
                      <span>{item.title}</span>
                    </SidebarMenuButton>
                  </SidebarMenuItem>
                ))}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>
      <SidebarFooter>
        <NavUser />
      </SidebarFooter>
      <SidebarRail />
    </Sidebar>
  );
}
