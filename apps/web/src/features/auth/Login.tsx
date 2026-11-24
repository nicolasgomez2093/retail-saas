import type { FormEvent } from 'react';
import type { Credentials } from '../../shared/api/client';

export function Login({ busy, onLogin }: { busy: boolean; onLogin: (value: Credentials) => void }) {
  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const form = new FormData(event.currentTarget);
    onLogin({
      email: String(form.get('email')),
      password: String(form.get('password')),
    });
  }

  return (
    <form onSubmit={submit}>
      <h1>Iniciar sesión</h1>
      <label>Correo electrónico<input name="email" type="email" autoComplete="username" required maxLength={254} /></label>
      <label>Contraseña<input name="password" type="password" autoComplete="current-password" required maxLength={1024} /></label>
      <button disabled={busy}>Ingresar</button>
    </form>
  );
}
