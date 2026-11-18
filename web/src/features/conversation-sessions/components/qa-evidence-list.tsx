/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useState } from "react";
import { useTranslation } from "react-i18next";
import { Link } from "@tanstack/react-router";
import { Button } from "@/components/ui/button";
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from "@/components/ui/collapsible";
import { formatNumber } from "@/lib/format";
import { toIntlLocale } from "@/i18n/languages";
import type { QAObservation } from "../qa-api";
import { ConversationContent } from "./conversation-content";

const states: Record<string, string> = {
  text: "Text",
  client_aux: "Client-added input",
  partial: "Partial input",
  redacted: "Credentials masked",
  capacity: "Capture capacity exceeded",
  unavailable: "Capture unavailable",
  input_limit: "Input size limit",
  continuation: "Continuation",
  no_new_input: "No new prompt",
  no_input: "No new prompt",
  final: "Final reply",
  final_redacted: "Credentials masked",
  http_error: "Request failed",
  upstream_failed: "Upstream failure",
  cancelled: "Cancelled",
  no_terminal: "No confirmed final reply",
  no_final: "No confirmed final reply",
  output_limit: "Output size limit",
  invalid_output: "Unrecognized response",
  open: "Completion not observed",
  legacy: "Historical observation",
  legacy_unknown: "Not recorded historically",
  legacy_turn: "Historical turn reply",
  legacy_input: "Historical observation",
  ambiguous: "Conflicting evidence",
  orphan_final: "Unpaired final reply",
  client: "Client chat ID",
  parent: "Parent response",
  tool: "Continuation",
  direct: "Input",
  submission: "Submission",
};
function foldRows(rows: QAObservation[], search?: boolean) {
  const groups: QAObservation[][] = [];
  for (const r of rows) {
    const lastGroup = groups.at(-1);
    const last = lastGroup?.[0];
    if (
      !search &&
      last &&
      r.chat_key &&
      last.user_id === r.user_id &&
      last.chat_key === r.chat_key &&
      r.prompt &&
      r.prompt === last.prompt &&
      r.input_state === last.input_state &&
      ["text", "legacy_input", "client_aux"].includes(r.input_state)
    ) {
      lastGroup?.push(r);
    } else groups.push([r]);
  }
  return groups;
}
function EvidenceRow(props: { row: QAObservation }) {
  const { t, i18n } = useTranslation();
  const r = props.row;
  const when = new Date(r.created_at * 1000).toLocaleString(
    toIntlLocale(i18n?.resolvedLanguage ?? "en"),
  );
  return (
    <article
      id={`qa-evidence-${r.id}`}
      className="min-w-0 space-y-3 rounded-lg border p-3"
    >
      <div className="text-muted-foreground flex flex-wrap gap-2 text-xs">
        <span>#{r.id}</span>
        <time>{when}</time>
        <span>
          {t("User ID")}: {r.user_id || t("Unknown")}
        </span>
        <span>{r.protocol}</span>
        {!!r.http_status && <span>HTTP {r.http_status}</span>}
        <span>{t(states[r.outcome] ?? r.outcome)}</span>
        {r.basis === "ambiguous" || r.basis === "orphan_final" ? (
          <span>{t(states[r.basis])}</span>
        ) : null}
      </div>
      {r.chat_key ? (
        <Link
          className="text-primary text-xs underline"
          to="/conversation-sessions/$sessionId"
          params={{ sessionId: r.chat_key }}
        >
          {t("Conversation")} ·{" "}
          {t(states[r.chat_basis] ?? "Historical observation")}
        </Link>
      ) : (
        <span className="text-muted-foreground text-xs">
          {t("Unassigned exchanges")}
        </span>
      )}
      {r.prompt && (
        <ConversationContent label={t("Request input")} value={r.prompt} />
      )}
      {r.input_state !== "text" && (
        <span className="text-muted-foreground text-xs">
          {t(states[r.input_state] ?? r.input_state)}
        </span>
      )}
      {r.final && (
        <ConversationContent label={t("Final reply")} value={r.final} />
      )}
      <div className="text-muted-foreground break-all text-xs">
        {t("Request ID")}: {r.request_id}
      </div>
    </article>
  );
}
function EvidenceGroup(props: { rows: QAObservation[]; all: boolean }) {
  const { t, i18n } = useTranslation();
  const [open, setOpen] = useState(false);
  const rows = props.rows;
  const locale = toIntlLocale(i18n?.resolvedLanguage ?? "en");
  if (rows.length === 1) return <EvidenceRow row={rows[0]} />;
  const statuses = new Map<string, number>();
  for (const r of rows) {
    const k = r.http_status
      ? `HTTP ${r.http_status}`
      : t(states[r.outcome] ?? r.outcome);
    statuses.set(k, (statuses.get(k) ?? 0) + 1);
  }
  const time = (v: number) => new Date(v * 1000).toLocaleString(locale);
  return (
    <Collapsible
      open={props.all || open}
      onOpenChange={setOpen}
      className="space-y-2 rounded-lg border p-3"
    >
      <CollapsibleTrigger render={<Button variant="outline" size="sm" />}>
        <span>{t("Repeated requests")}</span> ×{" "}
        {formatNumber(rows.length, locale)}
      </CollapsibleTrigger>
      <div className="text-muted-foreground text-xs">
        {time(
          Math.min(rows[0].created_at, (rows.at(-1) ?? rows[0]).created_at),
        )}{" "}
        –{" "}
        {time(
          Math.max(rows[0].created_at, (rows.at(-1) ?? rows[0]).created_at),
        )}{" "}
        · {[...statuses].map(([k, n]) => `${k} × ${n}`).join(" · ")} ·{" "}
        {t("Final reply")}: {rows.filter((r) => r.final).length}
      </div>
      <p className="line-clamp-2 text-sm whitespace-pre-wrap">
        {rows[0].prompt.slice(0, 180)}
      </p>
      <CollapsibleContent className="space-y-2">
        {rows.map((r) => (
          <EvidenceRow key={r.id} row={r} />
        ))}
      </CollapsibleContent>
    </Collapsible>
  );
}
export function EvidenceList(props: {
  rows: QAObservation[];
  search?: boolean;
}) {
  const { t } = useTranslation();
  const [all, setAll] = useState(false);
  const groups = foldRows(props.rows, props.search);
  return (
    <div className="space-y-3">
      {groups.some((g) => g.length > 1) && (
        <Button size="sm" variant="outline" onClick={() => setAll(!all)}>
          {t(all ? "Collapse all" : "Expand all")}
        </Button>
      )}
      {groups.map((g) => (
        <EvidenceGroup key={`${g[0].id}-${all}`} rows={g} all={all} />
      ))}
    </div>
  );
}
