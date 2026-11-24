/** Presentation only: HTTP status and machine codes remain unchanged. */
export function httpErrorMessage(status: number, supplied?: string) {
  if (status === 401) return 'La sesión venció. Volvé a iniciar sesión.';
  if (status === 403) return 'No tenés permiso para realizar esta operación.';
  if (status >= 500) return 'El servicio no está disponible en este momento.';
  return supplied || 'No pudimos completar la solicitud.';
}
