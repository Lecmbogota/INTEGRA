import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'

export default defineConfig({
  plugins: [react()],
  server: {
    // 5173 (el puerto por defecto de Vite) cae dentro de un rango que Windows
    // reserva para Hyper-V, igual que 5432/5433 para PostgreSQL. Compruébalo
    // con: netsh interface ipv4 show excludedportrange protocol=tcp
    port: 5580,
    // Se fija IPv4: al escuchar en ::1 la reserva de puertos también bloquea.
    host: '127.0.0.1',
    strictPort: true,
    // El proxy evita CORS en desarrollo: el navegador solo habla con Vite y
    // este reenvía /api al backend en Go.
    proxy: {
      '/api': { target: 'http://localhost:8080', changeOrigin: true },
      // Las imágenes del banco se sirven por hash fuera de /api; sin esta
      // entrada, Vite responde con el index.html y las miniaturas salen rotas.
      '/imagenes': { target: 'http://localhost:8080', changeOrigin: true },
    },
  },
})
