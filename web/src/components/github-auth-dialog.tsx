import { useEffect, useState, useCallback } from "react";
import { api } from "@/lib/api";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Input } from "@/components/ui/input";
import { Loader2, ExternalLink, Copy, Check, ShieldCheck } from "lucide-react";

type AuthState = "starting" | "pending" | "complete" | "expired" | "error";

interface GithubAuthDialogProps {
  open: boolean;
  onClose: () => void;
}

export function GithubAuthDialog({ open, onClose }: GithubAuthDialogProps) {
  const [state, setState] = useState<AuthState>("starting");
  const [userCode, setUserCode] = useState("");
  const [verifyUrl, setVerifyUrl] = useState("");
  const [errorMsg, setErrorMsg] = useState("");
  const [copied, setCopied] = useState(false);
  const [manualToken, setManualToken] = useState("");
  const [manualTokenError, setManualTokenError] = useState("");
  const [savingManualToken, setSavingManualToken] = useState(false);

  const startFlow = useCallback(async () => {
    setState("starting");
    setErrorMsg("");
    setCopied(false);
    setManualTokenError("");
    try {
      const res = await api.githubAuth.start();
      if (res.status === "complete") {
        setState("complete");
        return;
      }
      setUserCode(res.userCode);
      setVerifyUrl(res.verificationUri);
      setState("pending");
    } catch (err) {
      setState("error");
      setErrorMsg(err instanceof Error ? err.message : String(err));
    }
  }, []);

  // Start the flow on open
  useEffect(() => {
    if (open) {
      startFlow();
    }
    return () => {
      setState("starting");
      setUserCode("");
      setVerifyUrl("");
      setErrorMsg("");
      setCopied(false);
      setManualToken("");
      setManualTokenError("");
      setSavingManualToken(false);
    };
  }, [open, startFlow]);

  // Poll for completion
  useEffect(() => {
    if (!open || state !== "pending") return;
    let cancelled = false;

    const poll = async () => {
      try {
        const res = await api.githubAuth.status();
        if (cancelled) return;
        if (res.status === "complete") {
          setState("complete");
        } else if (res.status === "expired" || res.status === "error") {
          setState("error");
          setErrorMsg(
            res.status === "expired"
              ? "Authorization timed out. Please try again."
              : "Authorization failed.",
          );
        }
      } catch {
        // ignore polling errors — retry on next tick
      }
    };

    const interval = setInterval(poll, 5000);
    return () => {
      cancelled = true;
      clearInterval(interval);
    };
  }, [open, state]);

  const handleCopy = async () => {
    try {
      await navigator.clipboard.writeText(userCode);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // clipboard API may not be available
    }
  };

  const handleManualTokenSave = async () => {
    setManualTokenError("");
    if (!manualToken.trim()) {
      setManualTokenError("Please paste a GitHub token.");
      return;
    }

    setSavingManualToken(true);
    try {
      const res = await api.githubAuth.setToken(manualToken.trim());
      if (res.status === "complete") {
        setState("complete");
      } else {
        setManualTokenError("Token saved, but auth status is not complete yet.");
      }
    } catch (err) {
      setManualTokenError(err instanceof Error ? err.message : String(err));
    } finally {
      setSavingManualToken(false);
    }
  };

  return (
    <Dialog open={open} onOpenChange={(v) => !v && onClose()}>
      <DialogContent className="sm:max-w-md">
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <svg
              className="h-5 w-5"
              viewBox="0 0 24 24"
              fill="currentColor"
              aria-hidden="true"
            >
              <path d="M12 .297c-6.63 0-12 5.373-12 12 0 5.303 3.438 9.8 8.205 11.385.6.113.82-.258.82-.577 0-.285-.01-1.04-.015-2.04-3.338.724-4.042-1.61-4.042-1.61C4.422 18.07 3.633 17.7 3.633 17.7c-1.087-.744.084-.729.084-.729 1.205.084 1.838 1.236 1.838 1.236 1.07 1.835 2.809 1.305 3.495.998.108-.776.417-1.305.76-1.605-2.665-.3-5.466-1.332-5.466-5.93 0-1.31.465-2.38 1.235-3.22-.135-.303-.54-1.523.105-3.176 0 0 1.005-.322 3.3 1.23.96-.267 1.98-.399 3-.405 1.02.006 2.04.138 3 .405 2.28-1.552 3.285-1.23 3.285-1.23.645 1.653.24 2.873.12 3.176.765.84 1.23 1.91 1.23 3.22 0 4.61-2.805 5.625-5.475 5.92.42.36.81 1.096.81 2.22 0 1.606-.015 2.896-.015 3.286 0 .315.21.69.825.57C20.565 22.092 24 17.592 24 12.297c0-6.627-5.373-12-12-12" />
            </svg>
            Connect GitHub
          </DialogTitle>
          <DialogDescription>
            Authorize Sympozium to create Issues and Pull Requests on your
            behalf.
          </DialogDescription>
        </DialogHeader>

        <div className="space-y-4 py-2">
          {state === "starting" && (
            <div className="flex items-center justify-center gap-2 py-6 text-muted-foreground">
              <Loader2 className="h-5 w-5 animate-spin" />
              <span>Starting authorization…</span>
            </div>
          )}

          {state === "pending" && (
            <>
              <div className="rounded-lg border bg-muted/30 p-4 text-center space-y-3">
                <p className="text-sm text-muted-foreground">
                  Enter this code at GitHub:
                </p>
                <div className="flex items-center justify-center gap-2">
                  <code className="text-3xl font-bold tracking-widest font-mono">
                    {userCode}
                  </code>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-8 w-8"
                    onClick={handleCopy}
                  >
                    {copied ? (
                      <Check className="h-4 w-4 text-green-500" />
                    ) : (
                      <Copy className="h-4 w-4 text-muted-foreground" />
                    )}
                  </Button>
                </div>
              </div>

              <Button
                className="w-full"
                variant="outline"
                onClick={() => window.open(verifyUrl, "_blank")}
              >
                <ExternalLink className="mr-2 h-4 w-4" />
                Open {verifyUrl}
              </Button>

              <div className="flex items-center justify-center gap-2 text-sm text-muted-foreground">
                <Loader2 className="h-4 w-4 animate-spin" />
                Waiting for authorization…
              </div>
            </>
          )}

          {state === "complete" && (
            <div className="flex flex-col items-center gap-3 py-6">
              <ShieldCheck className="h-10 w-10 text-green-500" />
              <p className="text-sm font-medium">
                GitHub authenticated successfully
              </p>
              <p className="text-xs text-muted-foreground">
                Token saved to cluster. You can close this dialog.
              </p>
              <Button variant="outline" onClick={onClose}>
                Done
              </Button>
            </div>
          )}

          {state === "error" && (
            <div className="flex flex-col items-center gap-3 py-6">
              <Badge variant="destructive" className="text-sm">
                Error
              </Badge>
              <p className="text-sm text-muted-foreground text-center">
                {errorMsg || "Something went wrong."}
              </p>
              <Button variant="outline" onClick={startFlow}>
                Try Again
              </Button>
            </div>
          )}

          {(state === "pending" || state === "error") && (
            <div className="rounded-lg border bg-muted/20 p-4 space-y-3">
              <p className="text-sm font-medium">Use personal access token instead</p>
              <p className="text-xs text-muted-foreground">
                Paste a GitHub token with repo access. It will be stored as
                <code className="mx-1">GH_TOKEN</code> in the cluster secret used by the
                github-gitops skill.
              </p>
              <Input
                type="password"
                value={manualToken}
                onChange={(e) => setManualToken(e.target.value)}
                placeholder="github_pat_..."
              />
              {manualTokenError && (
                <p className="text-xs text-destructive">{manualTokenError}</p>
              )}
              <Button
                className="w-full"
                variant="secondary"
                onClick={handleManualTokenSave}
                disabled={savingManualToken}
              >
                {savingManualToken ? "Saving token…" : "Save token"}
              </Button>
            </div>
          )}
        </div>
      </DialogContent>
    </Dialog>
  );
}
