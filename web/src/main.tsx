import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import App from './App'
import './styles.css'
import './escritorio/pantalla.css'
import './escritorio/ventanas.css'
import './escritorio/escritorio.css'
import './escritorio/apps.css'
import './escritorio/configuracion.css'
import './escritorio/tema.css'

const raiz = document.getElementById('root')
if (!raiz) throw new Error('falta el elemento #root en index.html')

createRoot(raiz).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
