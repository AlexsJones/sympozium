import { useState, useEffect, useMemo } from "react";
import { useGatewayConfig, usePatchGatewayConfig, useCreateGatewayConfig, useInstances } from "@/hooks/use-api";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
  CardDescription,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "sonner";

export function GatewayPage() {
  const { data, isLoading } = useGatewayConfig();
  const patchMutation = usePatchGatewayConfig();
  const createMutation = useCreateGatewayConfig();

  const [form, setForm] = useState({
    enabled: false,
    gatewayClassName: "sympozium",
    name: "sympozium-gateway",
    baseDomain: "",
    tlsEnabled: false,
    certManagerClusterIssuer: "",
    tlsSecretName: "sympozium-wildcard-cert",
  });
  const [dirty, setDirty] = useState(false);

  // Sync form state when data loads
  useEffect(() => {
    if (data) {
      setForm({
        enabled: data.enabled,
        gatewayClassName: data.gatewayClassName || "sympozium",
        name: data.name || "sympozium-gateway",
        baseDomain: data.baseDomain || "",
        tlsEnabled: data.tlsEnabled,
        certManagerClusterIssuer: data.certManagerClusterIssuer || "",
        tlsSecretName: data.tlsSecretName || "sympozium-wildcard-cert",
      });
      setDirty(false);
    }
  }, [data]);

  const update = (patch: Partial<typeof form>) => {
    setForm((prev) => ({ ...prev, ...patch }));
    setDirty(true);
  };

  const handleSave = async () => {
    const isNew = !data?.phase;
    try {
      if (isNew) {
        await createMutation.mutateAsync(form);
      } else {
        await patchMutation.mutateAsync(form);
      }
      toast.success("Gateway configuration saved");
      setDirty(false);
    } catch {
      // Error toast handled by mutation hook
    }
  };

  const phase = data?.phase || "Not Configured";

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold">Gateway</h1>
        <p className="text-sm text-muted-foreground">
          Manage the shared Envoy Gateway for instance web endpoints
        </p>
      </div>

      {/* Status */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Status</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3 text-sm">
          {isLoading ? (
            <div className="space-y-2">
              <Skeleton className="h-5 w-full" />
              <Skeleton className="h-5 w-3/4" />
            </div>
          ) : (
            <>
              <div className="flex items-center justify-between">
                <span className="text-muted-foreground">Phase</span>
                <PhaseBadge phase={phase} />
              </div>
              {data?.message && (
                <p className="text-xs text-destructive">{data.message}</p>
              )}
              {data?.address && (
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Address</span>
                  <span className="font-mono">{data.address}</span>
                </div>
              )}
              {data?.listenerCount != null && data.listenerCount > 0 && (
                <div className="flex items-center justify-between">
                  <span className="text-muted-foreground">Listeners</span>
                  <span>{data.listenerCount}</span>
                </div>
              )}
            </>
          )}
        </CardContent>
      </Card>

      {/* Configuration */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Configuration</CardTitle>
          <CardDescription>
            Enable the gateway to expose instance web endpoints via Envoy
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {isLoading ? (
            <div className="space-y-3">
              <Skeleton className="h-9 w-full" />
              <Skeleton className="h-9 w-full" />
              <Skeleton className="h-9 w-full" />
            </div>
          ) : (
            <>
              <div className="flex items-center justify-between">
                <Label>Enabled</Label>
                <Button
                  variant={form.enabled ? "default" : "secondary"}
                  size="sm"
                  onClick={() => update({ enabled: !form.enabled })}
                >
                  {form.enabled ? "On" : "Off"}
                </Button>
              </div>

              <div className="space-y-2">
                <Label htmlFor="gw-baseDomain">Base Domain</Label>
                <Input
                  id="gw-baseDomain"
                  placeholder="sympozium.example.com"
                  value={form.baseDomain}
                  onChange={(e) => update({ baseDomain: e.target.value })}
                />
                <p className="text-xs text-muted-foreground">
                  Instances will be available at &lt;name&gt;.{form.baseDomain || "<baseDomain>"}
                </p>
              </div>

              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label htmlFor="gw-className">GatewayClass Name</Label>
                  <Input
                    id="gw-className"
                    value={form.gatewayClassName}
                    onChange={(e) => update({ gatewayClassName: e.target.value })}
                  />
                </div>
                <div className="space-y-2">
                  <Label htmlFor="gw-name">Gateway Name</Label>
                  <Input
                    id="gw-name"
                    value={form.name}
                    onChange={(e) => update({ name: e.target.value })}
                  />
                </div>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* TLS */}
      <Card>
        <CardHeader>
          <CardTitle className="text-sm">TLS</CardTitle>
          <CardDescription>
            Configure HTTPS with cert-manager for automatic certificate
            provisioning
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          {isLoading ? (
            <div className="space-y-3">
              <Skeleton className="h-9 w-full" />
              <Skeleton className="h-9 w-full" />
            </div>
          ) : (
            <>
              <div className="flex items-center justify-between">
                <Label>Enable TLS</Label>
                <Button
                  variant={form.tlsEnabled ? "default" : "secondary"}
                  size="sm"
                  onClick={() => update({ tlsEnabled: !form.tlsEnabled })}
                >
                  {form.tlsEnabled ? "On" : "Off"}
                </Button>
              </div>

              {form.tlsEnabled && (
                <>
                  <div className="space-y-2">
                    <Label htmlFor="gw-issuer">cert-manager ClusterIssuer</Label>
                    <Input
                      id="gw-issuer"
                      placeholder="letsencrypt-prod"
                      value={form.certManagerClusterIssuer}
                      onChange={(e) =>
                        update({ certManagerClusterIssuer: e.target.value })
                      }
                    />
                  </div>
                  <div className="space-y-2">
                    <Label htmlFor="gw-secretName">TLS Secret Name</Label>
                    <Input
                      id="gw-secretName"
                      value={form.tlsSecretName}
                      onChange={(e) => update({ tlsSecretName: e.target.value })}
                    />
                  </div>
                </>
              )}
            </>
          )}
        </CardContent>
      </Card>

      {/* Routes */}
      <RoutesCard baseDomain={form.baseDomain} />

      {/* Save */}
      {!isLoading && (
        <div className="flex justify-end">
          <Button
            onClick={handleSave}
            disabled={!dirty || patchMutation.isPending || createMutation.isPending}
          >
            {patchMutation.isPending || createMutation.isPending
              ? "Saving..."
              : "Save"}
          </Button>
        </div>
      )}
    </div>
  );
}

function RoutesCard({ baseDomain }: { baseDomain: string }) {
  const { data: instances, isLoading } = useInstances();

  const routes = useMemo(() => {
    if (!instances) return [];
    return instances.filter((i) =>
      i.spec.skills?.some(
        (s) => s.skillPackRef === "web-endpoint" || s.skillPackRef === "skillpack-web-endpoint",
      ),
    );
  }, [instances]);

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">Routes</CardTitle>
        <CardDescription>
          HTTPRoutes created for instances with web endpoints enabled
        </CardDescription>
      </CardHeader>
      <CardContent>
        {isLoading ? (
          <div className="space-y-2">
            <Skeleton className="h-5 w-full" />
            <Skeleton className="h-5 w-3/4" />
          </div>
        ) : routes.length === 0 ? (
          <p className="text-sm text-muted-foreground">
            No routes — enable web endpoints on instances to create routes
          </p>
        ) : (
          <div className="space-y-1 text-sm">
            <div className="grid grid-cols-4 gap-2 font-medium text-muted-foreground text-xs pb-1 border-b">
              <span>Instance</span>
              <span>Hostname</span>
              <span>Status</span>
              <span>URL</span>
            </div>
            {routes.map((inst) => {
              const webSkill = inst.spec.skills?.find(
                (s) => s.skillPackRef === "web-endpoint" || s.skillPackRef === "skillpack-web-endpoint",
              );
              const hostname =
                webSkill?.params?.hostname ||
                (baseDomain ? `${inst.metadata.name}.${baseDomain}` : "-");
              return (
                <div
                  key={inst.metadata.name}
                  className="grid grid-cols-4 gap-2 py-1"
                >
                  <span className="font-medium truncate">
                    {inst.metadata.name}
                  </span>
                  <span className="font-mono text-xs truncate">
                    {hostname}
                  </span>
                  <Badge variant="secondary" className="w-fit">
                    Skill
                  </Badge>
                  <span className="font-mono text-xs truncate">-</span>
                </div>
              );
            })}
          </div>
        )}
      </CardContent>
    </Card>
  );
}

function PhaseBadge({ phase }: { phase: string }) {
  const variant =
    phase === "Ready"
      ? "default"
      : phase === "Error"
        ? "destructive"
        : "secondary";
  return <Badge variant={variant}>{phase}</Badge>;
}
