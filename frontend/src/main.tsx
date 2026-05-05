import React from 'react'
import {createRoot} from 'react-dom/client'
import './style.css'
import './stores/useThemeStore'   // side-effect: applies saved theme class to <html> before first paint
import './stores/useGlowColorsStore' // side-effect: injects glow keyframes before first render
import App from './App'

const container = document.getElementById('root')

const root = createRoot(container!)

root.render(
    <React.StrictMode>
        <App/>
    </React.StrictMode>
)
