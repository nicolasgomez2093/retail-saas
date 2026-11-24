/** Group a decimal string for display without converting it to a floating-point number. */
export function posPrice(value: string) {
  const [whole, fraction] = value.split('.');
  const grouped = whole.replace(/\B(?=(\d{3})+(?!\d))/g, '.');
  return fraction === undefined ? grouped : grouped + ',' + fraction;
}
