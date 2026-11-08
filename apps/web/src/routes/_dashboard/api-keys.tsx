import { useQuery, useQueryClient } from "@tanstack/react-query";
import { createFileRoute } from "@tanstack/react-router";
import { Copy, KeyRound, Pencil, Plus, Trash2 } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { PageHeader } from "@/components/page-header";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Input } from "@/components/ui/input";
import { Label } from "@/components/ui/label";
import { useCurrentUser } from "@/hooks/use-auth";
import { api } from "@/lib/api";

export const Route = createFileRoute("/_dashboard/api-keys")({ component: APIKeysPage });

interface APIKey {
  id: string;
  user_id: string;
  name: string;
  created_at: string;
  expires_at: string | null;
  last_used_at: string | null;
  is_active: boolean;
}

const queryKey = ["api-keys"];
function localDate(value: string | null) {
  if (!value) return "";
  const date = new Date(value);
  return new Date(date.getTime() - date.getTimezoneOffset() * 60000).toISOString().slice(0, 16);
}
function errorMessage(error: unknown) {
  return typeof error === "object" && error !== null && "detail" in error
    ? String(error.detail)
    : "API key request failed";
}

function APIKeysPage() {
  const { data: user } = useCurrentUser();
  const allowed = user?.role === "owner" || user?.role === "admin";
  const qc = useQueryClient();
  const keys = useQuery({
    queryKey: [...queryKey, user?.id],
    queryFn: () => api.get<APIKey[]>("/api/v1/api-keys"),
    enabled: allowed,
  });
  const [editorOpen, setEditorOpen] = useState(false);
  const [editing, setEditing] = useState<APIKey | null>(null);
  const [name, setName] = useState("");
  const [expiry, setExpiry] = useState("");
  const [revoke, setRevoke] = useState<APIKey | null>(null);
  // Never put the one-time secret in query/mutation caches or browser storage.
  const [secret, setSecret] = useState("");
  const [busy, setBusy] = useState(false);

  function openEditor(key: APIKey | null) {
    setEditing(key);
    setName(key?.name ?? "");
    setExpiry(localDate(key?.expires_at ?? null));
    setEditorOpen(true);
  }
  async function save() {
    setBusy(true);
    try {
      const input = {
        name: name.trim(),
        expires_at: expiry ? new Date(expiry).toISOString() : null,
      };
      if (editing) {
        await api.put(`/api/v1/api-keys/${editing.id}`, input);
        toast.success("API key updated");
      } else {
        const result = await api.post<{ api_key: APIKey; key: string }>("/api/v1/api-keys", input);
        setSecret(result.key);
      }
      setEditorOpen(false);
      await qc.invalidateQueries({ queryKey });
    } catch (error) {
      toast.error(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }
  async function confirmRevoke() {
    if (!revoke) return;
    setBusy(true);
    try {
      await api.delete(`/api/v1/api-keys/${revoke.id}`);
      setRevoke(null);
      toast.success("API key revoked");
      await qc.invalidateQueries({ queryKey });
    } catch (error) {
      toast.error(errorMessage(error));
    } finally {
      setBusy(false);
    }
  }

  if (!user) return <p className="text-muted-foreground">Loading...</p>;
  if (!allowed)
    return <p className="text-destructive">Only owners and admins can manage API keys.</p>;

  return (
    <div className="space-y-6">
      <PageHeader
        title="API Keys"
        description="Manage your automation credentials. Keys inherit your permissions."
        actions={
          <Button onClick={() => openEditor(null)}>
            <Plus className="h-4 w-4" />
            Create key
          </Button>
        }
      />
      <p className="text-sm text-muted-foreground">
        Only your own keys are shown. Keys cannot manage API keys or change your password or 2FA.
        Password, role and 2FA changes invalidate existing keys. Use HTTPS and set an expiry.
      </p>
      {keys.isPending && <p className="text-sm text-muted-foreground">Loading keys...</p>}
      {keys.isError && (
        <p role="alert" className="text-destructive">
          {errorMessage(keys.error)}
        </p>
      )}
      {keys.data?.length === 0 && <p className="text-sm text-muted-foreground">No API keys yet.</p>}
      {keys.data?.map((key) => {
        const expired = key.expires_at !== null && new Date(key.expires_at).getTime() <= Date.now();
        return (
          <Card key={key.id}>
            <CardContent className="flex flex-wrap items-center justify-between gap-4 p-5">
              <div className="space-y-2">
                <div className="flex items-center gap-2">
                  <KeyRound className="h-4 w-4" />
                  <span className="font-medium">{key.name}</span>
                  <Badge variant={key.is_active && !expired ? "secondary" : "outline"}>
                    {!key.is_active ? "Revoked" : expired ? "Expired" : "Active"}
                  </Badge>
                </div>
                <div className="text-xs text-muted-foreground">
                  Created {new Date(key.created_at).toLocaleString()} · Expires{" "}
                  {key.expires_at ? new Date(key.expires_at).toLocaleString() : "Never"}
                  <br />
                  Last used{" "}
                  {key.last_used_at ? new Date(key.last_used_at).toLocaleString() : "Never"}
                </div>
              </div>
              {key.is_active && (
                <div className="flex gap-2">
                  <Button variant="outline" size="sm" onClick={() => openEditor(key)}>
                    <Pencil className="h-3.5 w-3.5" />
                    Edit
                  </Button>
                  <Button variant="destructive" size="sm" onClick={() => setRevoke(key)}>
                    <Trash2 className="h-3.5 w-3.5" />
                    Revoke
                  </Button>
                </div>
              )}
            </CardContent>
          </Card>
        );
      })}
      <Dialog
        open={editorOpen}
        onOpenChange={(open) => {
          if (!busy) setEditorOpen(open);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editing ? "Edit API key" : "Create API key"}</DialogTitle>
            <DialogDescription>
              Choose a name and optional expiry (your local time). Empty expiry means no expiry.
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-2">
            <Label htmlFor="api-key-name">Name</Label>
            <Input
              id="api-key-name"
              maxLength={100}
              value={name}
              onChange={(e) => setName(e.target.value)}
              autoComplete="off"
            />
          </div>
          <div className="space-y-2">
            <Label htmlFor="api-key-expiry">Expires at</Label>
            <Input
              id="api-key-expiry"
              type="datetime-local"
              value={expiry}
              onChange={(e) => setExpiry(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button disabled={busy || !name.trim()} onClick={save}>
              {busy ? "Saving..." : "Save"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={Boolean(secret)}
        onOpenChange={(open) => {
          if (!open) setSecret("");
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Copy your API key</DialogTitle>
            <DialogDescription>
              This key is shown only once. Store it securely before closing.
            </DialogDescription>
          </DialogHeader>
          <Input
            aria-label="New API key"
            readOnly
            value={secret}
            className="font-mono"
            autoComplete="off"
          />
          <DialogFooter>
            <Button
              variant="outline"
              onClick={async () => {
                try {
                  await navigator.clipboard.writeText(secret);
                  toast.success("Copied");
                } catch {
                  toast.error("Could not copy. Select and copy the key manually.");
                }
              }}
            >
              <Copy className="h-4 w-4" />
              Copy
            </Button>
            <Button onClick={() => setSecret("")}>Done</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
      <Dialog
        open={Boolean(revoke)}
        onOpenChange={(open) => {
          if (!open && !busy) setRevoke(null);
        }}
      >
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Revoke {revoke?.name}?</DialogTitle>
            <DialogDescription>
              This cannot be undone. Requests using this key will be rejected.
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="destructive" disabled={busy} onClick={confirmRevoke}>
              {busy ? "Revoking..." : "Revoke key"}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
}
