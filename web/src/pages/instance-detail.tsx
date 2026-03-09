import { useState, useEffect } from "react";
import { useParams, Link, useSearchParams } from "react-router-dom";
import { useInstance } from "@/hooks/use-api";
import { StatusBadge } from "@/components/status-badge";
import { GithubAuthDialog } from "@/components/github-auth-dialog";
import { api, type SkillRef, type SympoziumInstance } from "@/lib/api";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Separator } from "@/components/ui/separator";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs";
import { ArrowLeft } from "lucide-react";
import { formatAge } from "@/lib/utils";

export function InstanceDetailPage() {
  const { name } = useParams<{ name: string }>();
  const [searchParams, setSearchParams] = useSearchParams();
  const allowedTabs = new Set(["overview", "channels", "skills", "memory", "web-endpoint"]);
  const paramTab = searchParams.get("tab");
  const [activeTab, setActiveTab] = useState<string>(
    paramTab && allowedTabs.has(paramTab) ? paramTab : "overview",
  );
  const connectGithub = searchParams.get("connect") === "github";
  const { data: inst, isLoading } = useInstance(name || "");

  useEffect(() => {
    if (paramTab && allowedTabs.has(paramTab)) {
      setActiveTab(paramTab);
      return;
    }
    setActiveTab("overview");
  }, [paramTab]);

  const handleConsumeConnect = () => {
    const next = new URLSearchParams(searchParams);
    next.delete("connect");
    setSearchParams(next, { replace: true });
  };

  if (isLoading) {
    return (
      <div className="space-y-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-64 w-full" />
      </div>
    );
  }

  if (!inst) {
    return <p className="text-muted-foreground">Instance not found</p>;
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-3">
        <Link to="/instances" className="text-muted-foreground hover:text-foreground">
          <ArrowLeft className="h-5 w-5" />
        </Link>
        <div>
          <h1 className="text-2xl font-bold font-mono">{inst.metadata.name}</h1>
          <p className="flex items-center gap-2 text-sm text-muted-foreground">
            Created {formatAge(inst.metadata.creationTimestamp)} ago
            <StatusBadge phase={inst.status?.phase} />
          </p>
        </div>
      </div>

      <Tabs value={activeTab} onValueChange={setActiveTab}>
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="channels">Channels</TabsTrigger>
          <TabsTrigger value="skills">Skills</TabsTrigger>
          <TabsTrigger value="memory">Memory</TabsTrigger>
          <TabsTrigger value="web-endpoint">Web Endpoint</TabsTrigger>
        </TabsList>

        <TabsContent value="overview">
          <div className="grid gap-4 md:grid-cols-2">
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Agent Configuration</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                <Row label="Model" value={inst.spec.agents?.default?.model} />
                <Row label="Base URL" value={inst.spec.agents?.default?.baseURL} />
                <Row label="Thinking" value={inst.spec.agents?.default?.thinking} />
                <Row label="Policy" value={inst.spec.policyRef} />
              </CardContent>
            </Card>

            <Card>
              <CardHeader>
                <CardTitle className="text-base">Status</CardTitle>
              </CardHeader>
              <CardContent className="space-y-3">
                <Row label="Phase" value={inst.status?.phase} />
                <Row label="Active Pods" value={String(inst.status?.activeAgentPods ?? 0)} />
                <Row label="Total Runs" value={String(inst.status?.totalAgentRuns ?? 0)} />
              </CardContent>
            </Card>

            {inst.spec.authRefs && inst.spec.authRefs.length > 0 && (
              <Card>
                <CardHeader>
                  <CardTitle className="text-base">Auth References</CardTitle>
                </CardHeader>
                <CardContent>
                  <div className="space-y-2">
                    {inst.spec.authRefs.map((ref, i) => (
                      <div
                        key={i}
                        className="flex items-center gap-2 text-sm"
                      >
                        <Badge variant="secondary">{ref.provider}</Badge>
                        <span className="font-mono text-muted-foreground">
                          {ref.secret}
                        </span>
                      </div>
                    ))}
                  </div>
                </CardContent>
              </Card>
            )}
          </div>
        </TabsContent>

        <TabsContent value="channels">
          <Card>
            <CardContent className="pt-6">
              {inst.spec.channels && inst.spec.channels.length > 0 ? (
                <div className="space-y-3">
                  {inst.spec.channels.map((ch, i) => {
                    const chStatus = inst.status?.channels?.find(
                      (s) => s.type === ch.type
                    );
                    return (
                      <div key={i} className="flex items-center justify-between rounded-lg border p-3">
                        <div className="flex items-center gap-3">
                          <Badge variant="outline" className="capitalize">{ch.type}</Badge>
                          {ch.configRef && (
                            <span className="text-xs text-muted-foreground font-mono">
                              secret: {ch.configRef.secret}
                            </span>
                          )}
                        </div>
                        <div className="flex items-center gap-2">
                          <StatusBadge phase={chStatus?.status} />
                          {chStatus?.message && (
                            <span className="text-xs text-muted-foreground">
                              {chStatus.message}
                            </span>
                          )}
                        </div>
                      </div>
                    );
                  })}
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">
                  No channels configured
                </p>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="skills">
          <SkillsTab
            skills={inst.spec.skills}
            autoOpenGithubAuth={connectGithub}
            onConsumeGithubPrompt={handleConsumeConnect}
          />
        </TabsContent>

        <TabsContent value="memory">
          <Card>
            <CardContent className="pt-6">
              {inst.spec.memory ? (
                <div className="space-y-3">
                  <Row label="Enabled" value={inst.spec.memory.enabled ? "Yes" : "No"} />
                  <Row label="Max Size" value={inst.spec.memory.maxSizeKB ? `${inst.spec.memory.maxSizeKB} KB` : "Default"} />
                  <Separator />
                  {inst.spec.memory.systemPrompt && (
                    <div>
                      <p className="text-sm font-medium mb-2">System Prompt</p>
                      <pre className="rounded bg-muted/50 p-3 text-xs whitespace-pre-wrap">
                        {inst.spec.memory.systemPrompt}
                      </pre>
                    </div>
                  )}
                </div>
              ) : (
                <p className="text-sm text-muted-foreground">
                  Memory not configured
                </p>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="web-endpoint">
          <WebEndpointTab inst={inst} />
        </TabsContent>
      </Tabs>
    </div>
  );
}

function WebEndpointTab({ inst }: { inst: SympoziumInstance }) {
  const webSkill = inst.spec.skills?.find(
    (s) => s.skillPackRef === "web-endpoint" || s.skillPackRef === "skillpack-web-endpoint",
  );

  if (webSkill) {
    return (
      <Card>
        <CardContent className="pt-6 space-y-3">
          <Row label="Rate Limit" value={`${webSkill.params?.rate_limit_rpm || "60"} req/min`} />
          <Row label="Hostname" value={webSkill.params?.hostname || "auto from gateway"} />
          <Separator />
          <p className="text-sm font-medium">Status</p>
          <p className="text-xs text-muted-foreground">
            The web-proxy runs as a server-mode AgentRun. Check the Runs page for
            a run in "Serving" phase with a Deployment and Service.
          </p>
        </CardContent>
      </Card>
    );
  }

  // Not enabled
  return (
    <Card>
      <CardContent className="pt-6">
        <p className="text-sm text-muted-foreground">
          Web endpoint is not enabled. Add the "web-endpoint" skill to expose this
          agent as an HTTP API.
        </p>
      </CardContent>
    </Card>
  );
}

function Row({ label, value }: { label: string; value?: string | null }) {
  return (
    <div className="flex items-center justify-between text-sm">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-mono">{value || "—"}</span>
    </div>
  );
}

function SkillsTab({
  skills,
  autoOpenGithubAuth,
  onConsumeGithubPrompt,
}: {
  skills?: SkillRef[];
  autoOpenGithubAuth?: boolean;
  onConsumeGithubPrompt?: () => void;
}) {
  const [authDialogOpen, setAuthDialogOpen] = useState(false);
  const [authStatus, setAuthStatus] = useState<string | null>(null);

  const hasGithubGitops = skills?.some(
    (sk) => sk.skillPackRef === "github-gitops",
  );
  const ghSkill = skills?.find((sk) => sk.skillPackRef === "github-gitops");

  // Check auth status when github-gitops is attached
  useEffect(() => {
    if (!hasGithubGitops) return;
    let cancelled = false;
    const check = async () => {
      try {
        const res = await api.githubAuth.status();
        if (!cancelled) setAuthStatus(res.status);
      } catch {
        if (!cancelled) setAuthStatus("unknown");
      }
    };
    check();
    const interval = setInterval(check, 15000);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [hasGithubGitops]);

  useEffect(() => {
    if (!autoOpenGithubAuth || !hasGithubGitops) return;
    setAuthDialogOpen(true);
    onConsumeGithubPrompt?.();
  }, [autoOpenGithubAuth, hasGithubGitops, onConsumeGithubPrompt]);

  return (
    <>
      <Card>
        <CardContent className="pt-6 space-y-4">
          {skills && skills.length > 0 ? (
            <div className="flex flex-wrap gap-2">
              {skills.map((sk, i) => (
                <Badge key={i} variant="secondary">
                  {sk.skillPackRef}
                  {sk.params?.repo && (
                    <span className="ml-1 text-xs text-muted-foreground">
                      → {sk.params.repo}
                    </span>
                  )}
                </Badge>
              ))}
            </div>
          ) : (
            <p className="text-sm text-muted-foreground">
              No skills attached
            </p>
          )}

          {hasGithubGitops && (
            <div
              className={`rounded-lg border p-4 ${
                authStatus === "complete"
                  ? "border-green-500/30 bg-green-500/5"
                  : "border-yellow-500/30 bg-yellow-500/5"
              }`}
            >
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-3">
                  <svg
                    className="h-5 w-5"
                    viewBox="0 0 24 24"
                    fill="currentColor"
                    aria-hidden="true"
                  >
                    <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12" />
                  </svg>
                  <div>
                    <p className="text-sm font-medium">GitHub GitOps</p>
                    {ghSkill?.params?.repo && (
                      <p className="text-xs text-muted-foreground">
                        {ghSkill.params.repo}
                      </p>
                    )}
                  </div>
                </div>
                <div className="flex items-center gap-2">
                  {authStatus === "complete" ? (
                    <Badge
                      variant="outline"
                      className="border-green-500/50 text-green-600"
                    >
                      Authenticated
                    </Badge>
                  ) : authStatus === "pending" ? (
                    <Badge
                      variant="outline"
                      className="border-yellow-500/50 text-yellow-600"
                    >
                      Awaiting auth…
                    </Badge>
                  ) : (
                    <button
                      onClick={() => setAuthDialogOpen(true)}
                      className="inline-flex items-center gap-1.5 rounded-md border border-yellow-500/50 bg-yellow-500/10 px-3 py-1.5 text-xs font-medium text-yellow-600 transition-colors hover:bg-yellow-500/20"
                    >
                      Connect GitHub
                    </button>
                  )}
                </div>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      <GithubAuthDialog
        open={authDialogOpen}
        onClose={() => setAuthDialogOpen(false)}
      />
    </>
  );
}
