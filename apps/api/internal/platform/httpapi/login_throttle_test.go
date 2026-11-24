package httpapi

import (
	"testing"
	"time"
)

func TestLoginThrottle(t *testing.T) {
	clock := time.Date(2026, 11, 23, 12, 0, 0, 0, time.UTC)
	throttle := newLoginThrottle(15 * time.Minute)
	throttle.now = func() time.Time { return clock }

	for i := 0; i < failuresPerAccountIP; i++ {
		if wait := throttle.Blocked("demo\x00ana@example.test", "203.0.113.7"); wait != 0 {
			t.Fatalf("intento %d bloqueado antes del límite: %s", i+1, wait)
		}
		throttle.Fail("demo\x00ana@example.test", "203.0.113.7")
	}
	if wait := throttle.Blocked("demo\x00ana@example.test", "203.0.113.7"); wait != 15*time.Minute {
		t.Fatalf("la misma cuenta desde la misma dirección debe esperar 15 minutos: %s", wait)
	}
	if wait := throttle.Blocked("demo\x00ana@example.test", "198.51.100.4"); wait != 0 {
		t.Fatalf("la persona real desde otra dirección no debe quedar bloqueada: %s", wait)
	}
	if wait := throttle.Blocked("demo\x00otra@example.test", "203.0.113.7"); wait != 0 {
		t.Fatalf("otra cuenta desde la misma dirección todavía puede intentar: %s", wait)
	}

	// Many accounts from one address hit the address budget.
	for i := failuresPerAccountIP; i < failuresPerClientAddr; i++ {
		throttle.Fail("demo\x00cuenta"+string(rune('a'+i%26))+"@example.test", "203.0.113.7")
	}
	if wait := throttle.Blocked("demo\x00nueva@example.test", "203.0.113.7"); wait == 0 {
		t.Fatal("una dirección que prueba muchas cuentas debe quedar frenada")
	}

	clock = clock.Add(15*time.Minute + time.Second)
	if wait := throttle.Blocked("demo\x00ana@example.test", "203.0.113.7"); wait != 0 {
		t.Fatalf("la ventana debe vencer: %s", wait)
	}

	throttle.Fail("demo\x00ana@example.test", "203.0.113.7")
	throttle.Succeed("demo\x00ana@example.test", "203.0.113.7")
	if entry := throttle.entries["pair:demo\x00ana@example.test\x00203.0.113.7"]; entry != nil {
		t.Fatalf("un ingreso correcto limpia los fallos de esa cuenta en esa dirección: %+v", entry)
	}
}
