import { useMutation, useQueryClient } from "@tanstack/react-query";
import { useState, type FormEvent } from "react";
import { useNavigate } from "react-router-dom";

import { postAPI, setCSRFToken } from "../api/client";
import styles from "./LoginPage.module.css";

export function LoginPage() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const login = useMutation({
    mutationFn: () =>
      postAPI<"LoginResponse">("/api/v1/session/login", { email, password }),
    onSuccess: async (result) => {
      setCSRFToken(result.csrf_token);
      await queryClient.invalidateQueries({ queryKey: ["session"] });
      navigate("/");
    },
  });
  function submit(event: FormEvent) {
    event.preventDefault();
    login.mutate();
  }
  return (
    <main className={styles.page}>
      <section className={styles.panel} aria-labelledby="login-title">
        <div className={styles.mark}>A</div>
        <p className={styles.eyebrow}>Axiom V1A research console</p>
        <h1 id="login-title">Owner access</h1>
        <p className={styles.note}>
          Production-public data and virtual execution only. No exchange
          credentials are accepted.
        </p>
        <form onSubmit={submit}>
          <label>
            Email
            <input
              type="email"
              autoComplete="username"
              required
              value={email}
              onChange={(event) => setEmail(event.target.value)}
            />
          </label>
          <label>
            Password
            <input
              type="password"
              autoComplete="current-password"
              required
              value={password}
              onChange={(event) => setPassword(event.target.value)}
            />
          </label>
          {login.isError && (
            <p className={styles.error} role="alert">
              Authentication failed or is temporarily unavailable.
            </p>
          )}
          <button type="submit" disabled={login.isPending}>
            {login.isPending ? "Verifying…" : "Enter console"}
          </button>
        </form>
        <div className={styles.lock}>REAL TRADING DISABLED</div>
      </section>
    </main>
  );
}
