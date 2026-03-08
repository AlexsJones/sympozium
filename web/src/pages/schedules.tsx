import { useState } from "react";
import { useSchedules, useDeleteSchedule, useCreateSchedule, useInstances } from "@/hooks/use-api";
import { StatusBadge } from "@/components/status-badge";
import {
  Table,
  TableHeader,
  TableRow,
  TableHead,
  TableBody,
  TableCell,
} from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { Textarea } from "@/components/ui/textarea";
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
  DialogDescription,
} from "@/components/ui/dialog";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Plus, Trash2 } from "lucide-react";
import { formatAge } from "@/lib/utils";

export function SchedulesPage() {
  const { data, isLoading } = useSchedules();
  const instances = useInstances();
  const deleteSchedule = useDeleteSchedule();
  const createSchedule = useCreateSchedule();
  const [open, setOpen] = useState(false);
  const [search, setSearch] = useState("");
  const [form, setForm] = useState({
    name: "",
    instanceRef: "",
    schedule: "*/5 * * * *",
    type: "heartbeat",
    task: "",
    suspend: false,
  });

  const filtered = (data || [])
    .filter((s) =>
      s.metadata.name.toLowerCase().includes(search.toLowerCase())
    )
    .sort((a, b) => a.metadata.name.localeCompare(b.metadata.name));

  const handleCreate = () => {
    createSchedule.mutate(form, {
      onSuccess: () => {
        setOpen(false);
        setForm({
          name: "",
          instanceRef: "",
          schedule: "*/5 * * * *",
          type: "heartbeat",
          task: "",
          suspend: false,
        });
      },
    });
  };

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Schedules</h1>
          <p className="text-sm text-muted-foreground">
            SympoziumSchedules — cron-based recurring agent runs
          </p>
        </div>
        <Dialog open={open} onOpenChange={setOpen}>
          <DialogTrigger asChild>
            <Button size="sm">
              <Plus className="mr-2 h-4 w-4" /> Create Schedule
            </Button>
          </DialogTrigger>
          <DialogContent>
            <DialogHeader>
              <DialogTitle>Create Schedule</DialogTitle>
              <DialogDescription>
                Create a recurring schedule for agent runs.
              </DialogDescription>
            </DialogHeader>
            <div className="space-y-4 pt-2">
              <div className="space-y-2">
                <Label>Name</Label>
                <Input
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  placeholder="my-heartbeat"
                />
              </div>
              <div className="space-y-2">
                <Label>Instance</Label>
                <Select
                  value={form.instanceRef}
                  onValueChange={(v) => setForm({ ...form, instanceRef: v })}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="Select instance" />
                  </SelectTrigger>
                  <SelectContent>
                    {(instances.data || []).map((inst) => (
                      <SelectItem
                        key={inst.metadata.name}
                        value={inst.metadata.name}
                      >
                        {inst.metadata.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label>Type</Label>
                  <Select
                    value={form.type}
                    onValueChange={(v) => setForm({ ...form, type: v })}
                  >
                    <SelectTrigger>
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="heartbeat">Heartbeat</SelectItem>
                      <SelectItem value="scheduled">Scheduled</SelectItem>
                      <SelectItem value="sweep">Sweep</SelectItem>
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label>Cron</Label>
                  <Input
                    value={form.schedule}
                    onChange={(e) =>
                      setForm({ ...form, schedule: e.target.value })
                    }
                    placeholder="*/5 * * * *"
                  />
                </div>
              </div>
              <div className="space-y-2">
                <Label>Task</Label>
                <Textarea
                  value={form.task}
                  onChange={(e) => setForm({ ...form, task: e.target.value })}
                  placeholder="Task for the scheduled run…"
                  rows={3}
                />
              </div>
              <Button
                className="w-full"
                onClick={handleCreate}
                disabled={
                  !form.name ||
                  !form.instanceRef ||
                  !form.schedule ||
                  createSchedule.isPending
                }
              >
                {createSchedule.isPending ? "Creating…" : "Create Schedule"}
              </Button>
            </div>
          </DialogContent>
        </Dialog>
      </div>

      <Input
        placeholder="Search schedules…"
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
          No schedules found
        </p>
      ) : (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Instance</TableHead>
              <TableHead>Cron</TableHead>
              <TableHead>Type</TableHead>
              <TableHead>Phase</TableHead>
              <TableHead>Next Run</TableHead>
              <TableHead>Age</TableHead>
              <TableHead className="w-20" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {filtered.map((sched) => (
              <TableRow key={sched.metadata.name}>
                <TableCell className="font-mono text-sm">
                  {sched.metadata.name}
                </TableCell>
                <TableCell className="text-sm">
                  {sched.spec.instanceRef}
                </TableCell>
                <TableCell className="font-mono text-xs">
                  {sched.spec.schedule}
                </TableCell>
                <TableCell>
                  <Badge variant="outline" className="text-xs capitalize">
                    {sched.spec.type || "scheduled"}
                  </Badge>
                </TableCell>
                <TableCell>
                  <StatusBadge
                    phase={
                      sched.spec.suspend
                        ? "Suspended"
                        : sched.status?.phase
                    }
                  />
                </TableCell>
                <TableCell className="text-xs text-muted-foreground">
                  {sched.status?.nextRunTime
                    ? new Date(sched.status.nextRunTime).toLocaleString()
                    : "—"}
                </TableCell>
                <TableCell className="text-sm text-muted-foreground">
                  {formatAge(sched.metadata.creationTimestamp)}
                </TableCell>
                <TableCell>
                  <Button
                    variant="ghost"
                    size="icon"
                    onClick={() =>
                      deleteSchedule.mutate(sched.metadata.name)
                    }
                    disabled={deleteSchedule.isPending}
                    title="Delete"
                  >
                    <Trash2 className="h-4 w-4 text-destructive" />
                  </Button>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
