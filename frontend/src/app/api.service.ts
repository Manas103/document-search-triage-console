import { Injectable } from '@angular/core';
import { Document, SearchFacets, SearchResponse } from './models';

// This service holds the session token in memory only, and attaches it to
// every call. It makes no authorization decisions of its own: every check
// here exists purely so the UI can hide buttons the user cannot use, and
// every one of those checks is re-run server side regardless, because this
// client is not trusted. See backend cmd/bypassaudit for the proof.
@Injectable({ providedIn: 'root' })
export class ApiService {
  private readonly base = (window as any).__API_BASE__ || 'http://localhost:8081';
  token: string | null = null;
  role: string | null = null;
  username: string | null = null;

  private authHeaders(): Record<string, string> {
    return this.token ? { Authorization: `Bearer ${this.token}` } : {};
  }

  async login(username: string): Promise<void> {
    const res = await fetch(`${this.base}/api/session`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username }),
    });
    if (!res.ok) {
      throw new Error(`login failed: ${res.status}`);
    }
    const body = await res.json();
    this.token = body.token;
    this.username = username;
    // The role shown in the UI is read back from this username's own
    // documents access, not asserted by the client: the simplest honest
    // signal is the audit-log probe, since only SUPERVISOR can read it.
    const probe = await fetch(`${this.base}/api/audit-log`, { headers: this.authHeaders() });
    this.role = probe.status === 200 ? 'SUPERVISOR' : 'ANALYST';
  }

  async logout(): Promise<void> {
    if (!this.token) return;
    await fetch(`${this.base}/api/session/logout`, { method: 'POST', headers: this.authHeaders() });
    this.token = null;
    this.role = null;
    this.username = null;
  }

  async search(facets: SearchFacets): Promise<SearchResponse> {
    const params = new URLSearchParams();
    Object.entries(facets).forEach(([k, v]) => {
      if (v) params.set(k, v);
    });
    params.set('size', '200');
    const res = await fetch(`${this.base}/api/search?${params.toString()}`, { headers: this.authHeaders() });
    if (!res.ok) {
      const body = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(`search rejected (${res.status}): ${body.error}`);
    }
    return res.json();
  }

  async getDocument(id: string): Promise<Document> {
    const res = await fetch(`${this.base}/api/documents/${encodeURIComponent(id)}`, { headers: this.authHeaders() });
    if (!res.ok) {
      const body = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(`fetch document rejected (${res.status}): ${body.error}`);
    }
    return res.json();
  }

  async triage(id: string, status: string): Promise<void> {
    const res = await fetch(`${this.base}/api/documents/${encodeURIComponent(id)}/triage`, {
      method: 'POST',
      headers: { ...this.authHeaders(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ status }),
    });
    if (!res.ok) {
      const body = await res.json().catch(() => ({ error: res.statusText }));
      throw new Error(`triage rejected (${res.status}): ${body.error}`);
    }
  }
}
