package store

import (
	"testing"
	"time"
)

// El cálculo de la próxima ejecución es puro y no toca la base: se prueba
// entero. Un error aquí no rompe nada visiblemente — simplemente Integra deja
// de correr sola, que es el peor fallo posible en un planificador.

func horario(hora string, dias ...int16) Horario {
	return Horario{Nombre: "prueba", Hora: hora, Zona: "America/Bogota", Dias: dias}
}

func bogota(t *testing.T, s string) time.Time {
	t.Helper()
	loc, err := time.LoadLocation("America/Bogota")
	if err != nil {
		t.Skip("sin base de datos de zonas horarias")
	}
	ts, err := time.ParseInLocation("2006-01-02 15:04", s, loc)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestProximaEsHoySiAunNoPasoLaHora(t *testing.T) {
	got, err := ProximaEjecucion(horario("22:00"), bogota(t, "2026-08-22 10:00"))
	if err != nil {
		t.Fatal(err)
	}
	if quiero := bogota(t, "2026-08-22 22:00"); !got.Equal(quiero) {
		t.Fatalf("se esperaba %s, se obtuvo %s", quiero, got)
	}
}

func TestProximaEsMananaSiLaHoraYaPaso(t *testing.T) {
	got, err := ProximaEjecucion(horario("02:00"), bogota(t, "2026-08-22 10:00"))
	if err != nil {
		t.Fatal(err)
	}
	if quiero := bogota(t, "2026-08-23 02:00"); !got.Equal(quiero) {
		t.Fatalf("se esperaba %s, se obtuvo %s", quiero, got)
	}
}

// Justo a la hora en punto cuenta como pasada: si no, el planificador podría
// dispararla dos veces en el mismo minuto.
func TestLaHoraExactaCuentaComoPasada(t *testing.T) {
	got, err := ProximaEjecucion(horario("10:00"), bogota(t, "2026-08-22 10:00"))
	if err != nil {
		t.Fatal(err)
	}
	if quiero := bogota(t, "2026-08-23 10:00"); !got.Equal(quiero) {
		t.Fatalf("se esperaba el día siguiente, se obtuvo %s", got)
	}
}

// Los días van en numeración ISO (1=lunes … 7=domingo), no la de Go, que
// empieza en domingo=0. Confundirlas correría las tareas un día entero.
func TestSoloLosDiasIndicados(t *testing.T) {
	// 2026-08-22 es sábado. Con "solo lunes" debe saltar al 24.
	got, err := ProximaEjecucion(horario("08:00", 1), bogota(t, "2026-08-22 10:00"))
	if err != nil {
		t.Fatal(err)
	}
	if quiero := bogota(t, "2026-08-24 08:00"); !got.Equal(quiero) {
		t.Fatalf("con «solo lunes» se esperaba %s, se obtuvo %s", quiero, got)
	}
}

func TestDomingoEsSieteNoCero(t *testing.T) {
	// 2026-08-22 es sábado; con "solo domingo" debe ser el 23.
	got, err := ProximaEjecucion(horario("08:00", 7), bogota(t, "2026-08-22 10:00"))
	if err != nil {
		t.Fatal(err)
	}
	if quiero := bogota(t, "2026-08-23 08:00"); !got.Equal(quiero) {
		t.Fatalf("domingo debe ser el 7: se esperaba %s, se obtuvo %s", quiero, got)
	}
}

func TestSinDiasCorreTodosLosDias(t *testing.T) {
	got, err := ProximaEjecucion(horario("08:00"), bogota(t, "2026-08-22 10:00"))
	if err != nil {
		t.Fatal(err)
	}
	if quiero := bogota(t, "2026-08-23 08:00"); !got.Equal(quiero) {
		t.Fatalf("sin días indicados debe correr a diario: %s", got)
	}
}

// La hora es la de la zona del horario, no la del servidor. Un servidor en
// Fráncfort tiene que disparar «las 2 de la mañana» a las 2 en Bogotá.
func TestLaZonaMandaSobreLaDelServidor(t *testing.T) {
	h := horario("02:00")
	// Las 09:00 UTC son las 04:00 en Bogotá: la hora de hoy ya pasó allí.
	desde := time.Date(2026, 8, 22, 9, 0, 0, 0, time.UTC)
	got, err := ProximaEjecucion(h, desde)
	if err != nil {
		t.Fatal(err)
	}
	if quiero := bogota(t, "2026-08-23 02:00"); !got.Equal(quiero) {
		t.Fatalf("se esperaba %s en Bogotá, se obtuvo %s", quiero, got)
	}
}

func TestZonaYHoraInvalidasFallan(t *testing.T) {
	if _, err := ProximaEjecucion(Horario{Hora: "02:00", Zona: "Marte/Olimpo"}, time.Now()); err == nil {
		t.Error("una zona inexistente debe dar error")
	}
	if _, err := ProximaEjecucion(Horario{Hora: "25:99", Zona: "America/Bogota"}, time.Now()); err == nil {
		t.Error("una hora inválida debe dar error")
	}
}
