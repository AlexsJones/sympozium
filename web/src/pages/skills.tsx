import { useState } from "react";
import { useSkills } from "@/hooks/use-api";
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
import { Separator } from "@/components/ui/separator";
import { Eye } from "lucide-react";
import { formatAge } from "@/lib/utils";
import type { SkillPack } from "@/lib/api";

export function SkillsPage() {
  const { data, isLoading } = useSkills();
  const [search, setSearch] = useState("");
  const [selected, setSelected] = useState<SkillPack | null>(null);

  const filtered = (data || []).filter((s) =>
    s.metadata.name.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div className="space-y-4">
      <div>
        <h1 className="text-2xl font-bold">Skills</h1>
        <p className="text-sm text-muted-foreground">
          SkillPacks — bundled instructions and sidecars mounted into agent pods
        </p>
      </div>

      <Input
        placeholder="Search skills…"
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
          No skills found
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Skills</TableHead>
              <TableHead>Has Sidecar</TableHead>
              <TableHead>RBAC</TableHead>
              <TableHead>Host Access</TableHead>
              <TableHead>Age</TableHead>
              <TableHead className="w-20" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.map((sk) => (
              <TableRow key={sk.metadata.name}>
                <TableCell className="font-mono text-sm">
                  {sk.metadata.name}
                </TableCell>
                <TableCell className="text-sm">
                  {sk.spec.skills?.length ?? 0}
                </TableCell>
                <TableCell>
                  {sk.spec.sidecar ? (
                    <Badge variant="default" className="text-xs">Yes</Badge>
                  ) : (
                    <span className="text-muted-foreground text-xs">No</span>
                  )}
                </TableCell>
                <TableCell>
                  {sk.spec.sidecar?.rbac ? (
                    <Badge variant="secondary" className="text-xs">
                      {sk.spec.sidecar.rbac.length} rules
                    </Badge>
                  ) : (
                    <span className="text-muted-foreground text-xs">—</span>
                  )}
                </TableCell>
                <TableCell>
                  {sk.spec.sidecar?.hostAccess?.enabled ? (
                    <Badge variant="destructive" className="text-xs">Enabled</Badge>
                  ) : (
                    <span className="text-muted-foreground text-xs">—</span>
                  )}
                </TableCell>
                <TableCell className="text-sm text-muted-foreground">
                  {formatAge(sk.metadata.creationTimestamp)}
                </TableCell>
                <TableCell>
                  <Dialog
                    open={selected?.metadata.name === sk.metadata.name}
                    onOpenChange={(open) => !open && setSelected(null)}
                  >
                    <DialogTrigger asChild>
                      <Button
                        variant="ghost"
                        size="icon"
                        onClick={() => setSelected(sk)}
                      >
                        <Eye className="h-4 w-4" />
                      </Button>
                    </DialogTrigger>
                    <DialogContent className="max-w-2xl max-h-[80vh] overflow-auto">
                      <DialogHeader>
                        <DialogTitle className="font-mono">
                          {sk.metadata.name}
                        </DialogTitle>
                      </DialogHeader>
                      <SkillDetail skill={sk} />
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

function SkillDetail({ skill }: { skill: SkillPack }) {
  return (
    <div className="space-y-4">
      {skill.spec.skills?.map((s, i) => (
        <Card key={i}>
          <CardHeader>
            <CardTitle className="text-sm">{s.name}</CardTitle>
          </CardHeader>
          <CardContent>
            <pre className="whitespace-pre-wrap text-xs rounded bg-muted/50 p-3 overflow-auto max-h-64">
              {s.content}
            </pre>
          </CardContent>
        </Card>
      ))}

      {skill.spec.sidecar && (
        <>
          <Separator />
          <Card>
            <CardHeader>
              <CardTitle className="text-sm">Sidecar Container</CardTitle>
            </CardHeader>
            <CardContent className="space-y-2 text-sm">
              <div className="flex justify-between">
                <span className="text-muted-foreground">Image</span>
                <span className="font-mono">{skill.spec.sidecar.image}</span>
              </div>
              {skill.spec.sidecar.command && (
                <div>
                  <p className="text-muted-foreground mb-1">Command</p>
                  <code className="text-xs bg-muted/50 rounded px-2 py-1">
                    {skill.spec.sidecar.command.join(" ")}
                  </code>
                </div>
              )}
            </CardContent>
          </Card>
        </>
      )}

      {skill.spec.sidecar?.rbac && (
        <>
          <Separator />
          <Card>
            <CardHeader>
              <CardTitle className="text-sm">RBAC Rules</CardTitle>
            </CardHeader>
            <CardContent>
              <pre className="text-xs font-mono whitespace-pre-wrap rounded bg-muted/50 p-3 overflow-auto max-h-64">
                {JSON.stringify(skill.spec.sidecar.rbac, null, 2)}
              </pre>
            </CardContent>
          </Card>
        </>
      )}

      {skill.spec.sidecar?.hostAccess?.enabled && (
        <>
          <Separator />
          <Card>
            <CardHeader>
              <CardTitle className="text-sm">Host Access</CardTitle>
            </CardHeader>
            <CardContent className="space-y-3 text-sm">
              <div className="flex flex-wrap gap-2">
                {skill.spec.sidecar.hostAccess.hostPID && (
                  <Badge variant="outline" className="text-xs">hostPID</Badge>
                )}
                {skill.spec.sidecar.hostAccess.hostNetwork && (
                  <Badge variant="outline" className="text-xs">hostNetwork</Badge>
                )}
                {skill.spec.sidecar.hostAccess.runAsRoot && (
                  <Badge variant="outline" className="text-xs">runAsRoot</Badge>
                )}
                {skill.spec.sidecar.hostAccess.privileged && (
                  <Badge variant="outline" className="text-xs">privileged</Badge>
                )}
              </div>
              {skill.spec.sidecar.hostAccess.mounts && skill.spec.sidecar.hostAccess.mounts.length > 0 && (
                <div>
                  <p className="text-muted-foreground mb-1">HostPath Mounts</p>
                  <pre className="text-xs font-mono whitespace-pre-wrap rounded bg-muted/50 p-3 overflow-auto max-h-64">
                    {JSON.stringify(skill.spec.sidecar.hostAccess.mounts, null, 2)}
                  </pre>
                </div>
              )}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}
