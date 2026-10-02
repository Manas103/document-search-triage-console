import { Component } from '@angular/core';
import { CommonModule } from '@angular/common';
import { FormsModule } from '@angular/forms';
import { ApiService } from './api.service';
import {
  CATEGORIES, CLASSIFICATIONS, Document, NEXT_STATUS_FOR_ROLE,
  PRIORITIES, REGIONS, SOURCE_TYPES, STATUSES,
} from './models';

@Component({
  selector: 'app-root',
  standalone: true,
  imports: [CommonModule, FormsModule],
  templateUrl: './app.component.html',
  styleUrl: './app.component.css',
})
export class AppComponent {
  readonly categories = CATEGORIES;
  readonly regions = REGIONS;
  readonly priorities = PRIORITIES;
  readonly sourceTypes = SOURCE_TYPES;
  readonly classifications = CLASSIFICATIONS;
  readonly statuses = STATUSES;

  usernameInput = 'analyst1';
  loginError = '';

  queryText = '';
  category = '';
  region = '';
  priority = '';
  sourceType = '';
  classification = '';
  status = '';

  results: Document[] = [];
  total = 0;
  searchError = '';
  searching = false;

  selected: Document | null = null;
  triageError = '';
  triageBusy = false;

  constructor(public api: ApiService) {}

  async login(): Promise<void> {
    this.loginError = '';
    try {
      await this.api.login(this.usernameInput.trim());
    } catch (e: any) {
      this.loginError = e.message;
    }
  }

  async logout(): Promise<void> {
    await this.api.logout();
    this.results = [];
    this.selected = null;
  }

  async search(): Promise<void> {
    this.searchError = '';
    this.searching = true;
    this.selected = null;
    try {
      const resp = await this.api.search({
        q: this.queryText || undefined,
        category: this.category || undefined,
        region: this.region || undefined,
        priority: this.priority || undefined,
        source_type: this.sourceType || undefined,
        classification: this.classification || undefined,
        status: this.status || undefined,
      });
      this.results = resp.results;
      this.total = resp.total;
    } catch (e: any) {
      this.searchError = e.message;
      this.results = [];
    } finally {
      this.searching = false;
    }
  }

  async open(doc: Document): Promise<void> {
    this.triageError = '';
    try {
      this.selected = await this.api.getDocument(doc.id);
    } catch (e: any) {
      this.triageError = e.message;
    }
  }

  // UX-only: the next status this client would offer for the current role.
  // The server never trusts this; it re-derives the same decision itself.
  nextStatus(): string | null {
    if (!this.selected || !this.api.role) return null;
    return NEXT_STATUS_FOR_ROLE[this.api.role]?.[this.selected.status] ?? null;
  }

  async advance(): Promise<void> {
    const next = this.nextStatus();
    if (!this.selected || !next) return;
    this.triageBusy = true;
    this.triageError = '';
    try {
      await this.api.triage(this.selected.id, next);
      const updated = await this.api.getDocument(this.selected.id);
      // search() clears `selected` as part of starting a fresh search; run
      // it first, then restore the just-updated detail so the drill-down
      // panel reflects the new status instead of disappearing.
      await this.search();
      this.selected = updated;
    } catch (e: any) {
      this.triageError = e.message;
    } finally {
      this.triageBusy = false;
    }
  }
}
