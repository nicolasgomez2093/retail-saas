const amountPattern = /^(0|[1-9][0-9]{0,15})(\.[0-9]{1,2})?$/;

export function decimalInput(raw: FormDataEntryValue | null): string {
  const value = String(raw ?? '').trim().replace(',', '.');
  if (!amountPattern.test(value)) throw new Error('Ingresá un importe válido con hasta dos decimales.');
  const [whole, decimal = ''] = value.split('.');
  return whole + '.' + decimal.padEnd(2, '0');
}

export function minorUnits(canonical: string): bigint {
  return BigInt(canonical.replace('.', ''));
}

export function decimalText(cents: bigint): string {
  if (cents < 0n) return '-' + decimalText(-cents);
  return String(cents / 100n) + '.' + String(cents % 100n).padStart(2, '0');
}
