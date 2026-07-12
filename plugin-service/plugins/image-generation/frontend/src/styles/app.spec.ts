import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { describe, expect, it } from 'vitest'

const css = readFileSync(resolve(process.cwd(), 'src/styles/app.css'), 'utf8')

describe('implicit scrollbars', () => {
  it('keeps conversation scroll regions scrollable while revealing their thumbs on hover', () => {
    expect(css).toMatch(/\.history-list,\s*\.chat-thread\s*\{[^}]*scrollbar-width:\s*thin;[^}]*scrollbar-color:\s*transparent transparent;/s)
    expect(css).toMatch(/\.history-list:hover,\s*\.chat-thread:hover\s*\{[^}]*scrollbar-color:\s*rgba\(100,\s*116,\s*139,\s*\.45\) transparent;/s)
    expect(css).toMatch(/:is\(\.history-list,\s*\.chat-thread\)::\-webkit-scrollbar\s*\{[^}]*width:\s*6px;/s)
    expect(css).toMatch(/:is\(\.history-list,\s*\.chat-thread\):hover::\-webkit-scrollbar-thumb\s*\{[^}]*background:\s*rgba\(100,\s*116,\s*139,\s*\.45\);/s)
  })
})
