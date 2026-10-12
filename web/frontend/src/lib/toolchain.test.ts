// @vitest-environment node
import { describe, expect, it } from 'vitest'
import { fileURLToPath } from 'node:url'
import tsconfig from '../../tsconfig.json'
import config from '../../vite.config'

describe('frontend toolchain contracts', () => {
  it('resolves the existing @ imports without the removed baseUrl option', () => {
    expect(tsconfig.compilerOptions).not.toHaveProperty('baseUrl')
    expect(tsconfig.compilerOptions.paths['@/*']).toEqual(['./src/*'])
    expect(config.resolve).toMatchObject({
      alias: { '@': fileURLToPath(new URL('../', import.meta.url)).replace(/\/$/, '') },
    })
  })

  it('does not raise the supported JavaScript browser floor with Vite defaults', () => {
    expect(config.build).toMatchObject({ target: ['chrome107', 'edge107', 'firefox104', 'safari16'] })
  })
})
