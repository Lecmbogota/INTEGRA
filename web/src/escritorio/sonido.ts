// Sonidos del sistema, sintetizados en el navegador.
//
// No hay ficheros de audio: un acorde corto se describe con cuatro osciladores
// y sus envolventes, pesa cero bytes y suena igual en todas partes. El de
// inicio de sesión es la firma sonora de Integra: cuatro notas ascendentes
// (Do–Mi–Sol–Do) con un fondo suave, un segundo largo, sin estridencias.
// Se puede apagar en Configuración (integra.sonido = 'off').

const CLAVE_SILENCIO = 'integra.sonido'

export function sonidoActivo(): boolean {
  try { return localStorage.getItem(CLAVE_SILENCIO) !== 'off' } catch { return true }
}

export function ponerSonido(activo: boolean) {
  try { localStorage.setItem(CLAVE_SILENCIO, activo ? 'on' : 'off') } catch { /* sin almacenamiento */ }
}

type Nota = { hz: number; en: number; dura: number; vol: number; tipo: OscillatorType }

function tocar(notas: Nota[], volumen = 0.22) {
  if (!sonidoActivo()) return
  const Ctx = window.AudioContext || (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext
  if (!Ctx) return
  const ctx = new Ctx()
  const maestro = ctx.createGain()
  maestro.gain.value = volumen
  // Un filtro paso bajo quita el brillo de los armónicos: suena a campana
  // suave y no a pitido.
  const filtro = ctx.createBiquadFilter()
  filtro.type = 'lowpass'
  filtro.frequency.value = 2600
  maestro.connect(filtro).connect(ctx.destination)

  const t0 = ctx.currentTime + 0.02
  for (const n of notas) {
    const osc = ctx.createOscillator()
    const env = ctx.createGain()
    osc.type = n.tipo
    osc.frequency.value = n.hz
    const ini = t0 + n.en
    env.gain.setValueAtTime(0, ini)
    env.gain.linearRampToValueAtTime(n.vol, ini + 0.02)
    env.gain.exponentialRampToValueAtTime(0.001, ini + n.dura)
    osc.connect(env).connect(maestro)
    osc.start(ini)
    osc.stop(ini + n.dura + 0.05)
  }
  const fin = Math.max(...notas.map((n) => n.en + n.dura)) + 0.2
  window.setTimeout(() => { void ctx.close() }, (fin + 0.1) * 1000)
}

// Inicio de sesión: arpegio ascendente con un colchón grave.
export function sonidoInicio() {
  tocar([
    { hz: 130.81, en: 0, dura: 1.4, vol: 0.35, tipo: 'sine' },       // Do3, colchón
    { hz: 523.25, en: 0, dura: 0.9, vol: 0.55, tipo: 'triangle' },   // Do5
    { hz: 659.25, en: 0.12, dura: 0.9, vol: 0.5, tipo: 'triangle' }, // Mi5
    { hz: 783.99, en: 0.24, dura: 1.0, vol: 0.5, tipo: 'triangle' }, // Sol5
    { hz: 1046.5, en: 0.36, dura: 1.2, vol: 0.45, tipo: 'sine' },    // Do6
  ])
}

// Cierre de sesión: dos notas descendentes, más corto.
export function sonidoCierre() {
  tocar([
    { hz: 659.25, en: 0, dura: 0.5, vol: 0.5, tipo: 'triangle' },
    { hz: 392.0, en: 0.16, dura: 0.7, vol: 0.5, tipo: 'sine' },
  ], 0.18)
}

// Aviso de notificación: un toque breve.
export function sonidoAviso() {
  tocar([
    { hz: 880, en: 0, dura: 0.25, vol: 0.5, tipo: 'sine' },
    { hz: 1174.7, en: 0.09, dura: 0.35, vol: 0.4, tipo: 'sine' },
  ], 0.14)
}
