import { usePolicies } from "@/hooks/use-api";
import { StatusBadge } from "@/components/status-badge";
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from "@/components/ui/table";
import {
  Card,
  CardHeader,
  CardTitle,
  CardContent,
} from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Eye } from "lucide-react";
import { formatAge } from "@/lib/utils";
import { useState } from "react";
import type { SympoziumPolicy } from "@/lib/api";

export function PoliciesPage() {
  const { data, isLoading } = usePolicies();
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<SympoziumPolicy | null>(null);

  const filtered = (data || [])
    .filter((p) =>
      p.metadata.name.toLowerCase().includes(search.toLowerCase())
    )
    .sort((a, b) => a.metadata.name.localeCompare(b.metadata.name));

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-bold">Policies</h1>
        <p className="text-sm text-muted-foreground">
          SympoziumPolicies — security and tool-gating rules enforced by the
          admission webhook
        </p>
      </div>

      <Input
        placeholder="Search policies…"
        value={search}
        onChange={(e) => setSearch(e.target.value)}
        className="max-w-sm"
      />

      {isLoading ? (
        <div className="space-y-2">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      ) : filtered.length === 0 ? (
        <p className="py-8 text-center text-muted-foreground">
          No policies found
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Tool Rules</TableHead>
              <TableHead>Sandbox</TableHead>
              <TableHead>Network</TableHead>
              <TableHead>Age</TableHead>
              <TableHead className="w-20" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.map((pol) => (
              <TableRow key={pol.metadata.name}>
                <TableCell className="font-mono text-sm">
                  {pol.metadata.name}
                </TableCell>
                <TableCell className="text-sm">
                  {pol.spec.toolGating?.rules?.length ?? "—"}
                </TableCell>
                <TableCell>
                  {pol.spec.sandboxPolicy?.required ? (
                    <Badge variant="default" className="bg-amber-600 text-xs">Required</Badge>
                  ) : (
                    <Badge variant="secondary" className="text-xs">Optional</Badge>
                  )}
                </TableCell>
                <TableCell>
                  {pol.spec.networkPolicy?.denyAll ? (
                    <Badge variant="destructive" className="text-xs">Isolated</Badge>
                  ) : (
                    <Badge variant="secondary" className="text-xs">Open</Badge>
                  )}
                </TableCell>
                <TableCell className="text-sm text-muted-foreground">
                  {formatAge(pol.metadata.creationTimestamp)}
                </TableCell>
                <TableCell>
                  <Dialog
                    open={selected?.metadata.name === pol.metadata.name}
                    onOpenChange={(open) => !open && setSelected(null)}
                  >
                    <DialogTrigger asChild>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => setSelected(pol)}
                      >
                        <Eye className="h-4 w-4" />
                      </Button>
                    </DialogTrigger>
                    <DialogContent className="max-w-2xl max-h-[80vh] overflow-auto">
                      <DialogHeader>
                        <DialogTitle className="font-mono">
                          {pol.metadata.name}
                        </DialogTitle>
                      </DialogHeader>
                      <PolicyDetail policy={pol} />
                    </DialogContent>
                  </Dialog>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}

function PolicyDetail({ policy }: { policy: SympoziumPolicy }) {
  const tg = policy.spec.toolGating;
  const sb = policy.spec.sandboxPolicy;
  const net = policy.spec.networkPolicy;

  return (
    <div className="space-y-4">
      {tg && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Tool Gating</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            {tg.defaultAction !== undefined && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">Default Action</span>
                <Badge variant={tg.defaultAction === "allow" ? "default" : "destructive"}>
                  {tg.defaultAction}
                </Badge>
              </div>
            )}
            {tg.rules && tg.rules.length > 0 && (
              <div>
                <p className="text-muted-foreground mb-1">Rules</p>
                <div className="space-y-1">
                  {tg.rules.map((r, i) => (
                    <div key={i} className="flex items-center gap-2">
                      <Badge variant="secondary" className="font-mono text-xs">
                        {r.tool}
                      </Badge>
                      <Badge
                        variant={r.action === "allow" ? "default" : "destructive"}
                        className="text-xs"
                      >
                        {r.action}
                      </Badge>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      {sb && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Sandbox Policy</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Required</span>
              <span>{sb.required ? "Yes" : "No"}</span>
            </div>
            {sb.defaultImage && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">Default Image</span>
                <span className="font-mono">{sb.defaultImage}</span>
              </div>
            )}
            {sb.maxCPU && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">Max CPU</span>
                <span className="font-mono">{sb.maxCPU}</span>
              </div>
            )}
            {sb.maxMemory && (
              <div className="flex justify-between">
                <span className="text-muted-foreground">Max Memory</span>
                <span className="font-mono">{sb.maxMemory}</span>
              </div>
            )}
            <div className="flex justify-between">
              <span className="text-muted-foreground">Allow Host Mounts</span>
              <span>{sb.allowHostMounts ? "Yes" : "No"}</span>
            </div>
          </CardContent>
        </Card>
      )}

      {net && (
        <Card>
          <CardHeader>
            <CardTitle className="text-sm">Network Policy</CardTitle>
          </CardHeader>
          <CardContent className="space-y-2 text-sm">
            <div className="flex justify-between">
              <span className="text-muted-foreground">Deny All</span>
              <Badge variant={net.denyAll ? "destructive" : "default"}>
                {net.denyAll ? "Yes" : "No"}
              </Badge>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Allow DNS</span>
              <span>{net.allowDNS ? "Yes" : "No"}</span>
            </div>
            <div className="flex justify-between">
              <span className="text-muted-foreground">Allow Event Bus</span>
              <span>{net.allowEventBus ? "Yes" : "No"}</span>
            </div>
            {net.allowedEgress && net.allowedEgress.length > 0 && (
              <div>
                <p className="text-muted-foreground mb-1">Allowed Egress</p>
                <div className="flex flex-wrap gap-1">
                  {net.allowedEgress.map((e, i) => (
                    <Badge key={i} variant="secondary" className="font-mono text-xs">
                      {e.host}:{e.port}
                    </Badge>
                  ))}
                </div>
              </div>
            )}
          </CardContent>
        </Card>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="text-sm">Raw Spec</CardTitle>
        </CardHeader>
        <CardContent>
          <pre className="text-xs font-mono whitespace-pre-wrap rounded bg-muted/50 p-3 overflow-auto max-h-64">
            {JSON.stringify(policy.spec, null, 2)}
          </pre>
        </CardContent>
      </Card>
    </div>
  );
}
