import { useState } from "react";
import { Outlet } from "react-router-dom";
import { AppSidebar } from "./sidebar";
import { Header } from "./header";
import { FeedPane } from "@/components/feed-pane";

export function Layout() {
  const [feedOpen, setFeedOpen] = useState(false);
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false);

  return (
    <div className="flex h-screen overflow-hidden">
      <AppSidebar
        collapsed={sidebarCollapsed}
        onToggle={() => setSidebarCollapsed((v) => !v)}
      />
      <div className="flex flex-1 flex-col overflow-hidden">
        <Header />
        <main className="flex-1 overflow-auto p-6">
          <Outlet />
        </main>
      </div>
      <FeedPane open={feedOpen} onToggle={() => setFeedOpen((v) => !v)} />
    </div>
  );
}
