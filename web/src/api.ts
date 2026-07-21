import type { Character, CharacterClass } from "./types";

export interface BuildInfo {
  time: string;
  commit: string;
}

export interface AccountInfo {
  email: string;
  is_admin: boolean;
}

export interface AdminSpawnRequest {
  character_id: string;
  spawn_id: string;
  x: number;
  y: number;
  config: Record<string, string>;
}

const tokenKey = "spellfire-session";

export class API {
  token = sessionStorage.getItem(tokenKey) ?? "";
  account: AccountInfo | null = null;

  async authenticate(mode: "login" | "register", email: string, password: string): Promise<void> {
    const result = await this.request<{ token: string; account: AccountInfo }>(`/api/auth/${mode}`, { method: "POST", body: JSON.stringify({ email, password }) }, false);
    this.token = result.token;
    this.account = result.account;
    sessionStorage.setItem(tokenKey, result.token);
  }

  version(): Promise<BuildInfo> {
    return this.request<BuildInfo>("/api/version", {}, false);
  }

  async logout(): Promise<void> {
    try { await this.request<void>("/api/auth/logout", { method: "POST" }); } finally { this.token = ""; this.account = null; sessionStorage.removeItem(tokenKey); }
  }

  async loadAccount(): Promise<AccountInfo> {
    const account = await this.request<AccountInfo>("/api/account");
    this.account = account;
    return account;
  }

  async characters(): Promise<Character[]> {
    const result = await this.request<{ characters: Character[] }>("/api/characters");
    return result.characters;
  }

  createCharacter(name: string, characterClass: CharacterClass): Promise<Character> {
    return this.request<Character>("/api/characters", { method: "POST", body: JSON.stringify({ name, class: characterClass }) });
  }

  adminSpawn(request: AdminSpawnRequest): Promise<void> {
    return this.request<void>("/api/admin/spawn", { method: "POST", body: JSON.stringify(request) });
  }

  adminAttributes(characterID: string, attributes: Record<string, number>): Promise<void> {
    return this.request<void>("/api/admin/attributes", { method: "POST", body: JSON.stringify({ character_id: characterID, attributes }) });
  }

  private async request<T>(path: string, init: RequestInit = {}, authenticated = true): Promise<T> {
    const headers = new Headers(init.headers);
    headers.set("Content-Type", "application/json");
    if (authenticated && this.token) headers.set("Authorization", `Bearer ${this.token}`);
    const response = await fetch(path, { ...init, headers });
    if (!response.ok) {
      const body = await response.json().catch(() => ({ error: "The service did not respond." })) as { error?: string };
      if (response.status === 401 && authenticated) { this.token = ""; this.account = null; sessionStorage.removeItem(tokenKey); }
      throw new Error(body.error ?? "The request failed.");
    }
    if (response.status === 204) return undefined as T;
    return response.json() as Promise<T>;
  }
}
