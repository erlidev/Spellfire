import type { Character, CharacterClass } from "./types";

const tokenKey = "spellfire-session";

export class API {
  token = sessionStorage.getItem(tokenKey) ?? "";

  async authenticate(mode: "login" | "register", email: string, password: string): Promise<void> {
    const result = await this.request<{ token: string }>(`/api/auth/${mode}`, { method: "POST", body: JSON.stringify({ email, password }) }, false);
    this.token = result.token;
    sessionStorage.setItem(tokenKey, result.token);
  }

  async logout(): Promise<void> {
    try { await this.request<void>("/api/auth/logout", { method: "POST" }); } finally { this.token = ""; sessionStorage.removeItem(tokenKey); }
  }

  async characters(): Promise<Character[]> {
    const result = await this.request<{ characters: Character[] }>("/api/characters");
    return result.characters;
  }

  createCharacter(name: string, characterClass: CharacterClass): Promise<Character> {
    return this.request<Character>("/api/characters", { method: "POST", body: JSON.stringify({ name, class: characterClass }) });
  }

  private async request<T>(path: string, init: RequestInit = {}, authenticated = true): Promise<T> {
    const headers = new Headers(init.headers);
    headers.set("Content-Type", "application/json");
    if (authenticated && this.token) headers.set("Authorization", `Bearer ${this.token}`);
    const response = await fetch(path, { ...init, headers });
    if (!response.ok) {
      const body = await response.json().catch(() => ({ error: "The service did not respond." })) as { error?: string };
      if (response.status === 401 && authenticated) { this.token = ""; sessionStorage.removeItem(tokenKey); }
      throw new Error(body.error ?? "The request failed.");
    }
    if (response.status === 204) return undefined as T;
    return response.json() as Promise<T>;
  }
}
