import { describe, it, expect, beforeEach, vi } from 'vitest'

describe('request interceptor - token injection', () => {
  beforeEach(() => {
    vi.resetModules()
    localStorage.clear()
  })

  it('adds Authorization header when token exists in localStorage', async () => {
    localStorage.setItem('token', 'test-token-123')
    const { default: api } = await import('../../src/api/request')
    const config = { headers: {} } as any
    const interceptor = (api.interceptors.request as any).handlers[0]
    const result = interceptor.fulfilled(config)
    expect(result.headers.Authorization).toBe('Bearer test-token-123')
  })

  it('does not add Authorization header when no token', async () => {
    const { default: api } = await import('../../src/api/request')
    const config = { headers: {} } as any
    const interceptor = (api.interceptors.request as any).handlers[0]
    const result = interceptor.fulfilled(config)
    expect(result.headers.Authorization).toBeUndefined()
  })
})

describe('response interceptor - 401 handling', () => {
  beforeEach(() => {
    vi.resetModules()
    localStorage.clear()
  })

  it('clears token and redirects to /login on 401', async () => {
    localStorage.setItem('token', 'expired-token')
    // Mock window.location before importing the module
    vi.stubGlobal('window', { location: { href: '' } })
    const { default: api } = await import('../../src/api/request')
    const errorHandler = (api.interceptors.response as any).handlers[0]
    const error = { response: { status: 401 } }
    try {
      await errorHandler.rejected(error)
    } catch (_) {
      // expected - Promise.reject
    }
    expect(localStorage.getItem('token')).toBeNull()
    expect(window.location.href).toBe('/login')
  })

  it('does not clear token on non-401 error', async () => {
    localStorage.setItem('token', 'valid-token')
    vi.stubGlobal('window', { location: { href: '' } })
    const { default: api } = await import('../../src/api/request')
    const errorHandler = (api.interceptors.response as any).handlers[0]
    const error = { response: { status: 500 } }
    try {
      await errorHandler.rejected(error)
    } catch (_) {
      // expected
    }
    expect(localStorage.getItem('token')).toBe('valid-token')
    expect(window.location.href).not.toBe('/login')
  })

  it('handles network error with no response object', async () => {
    const { default: api } = await import('../../src/api/request')
    const errorHandler = (api.interceptors.response as any).handlers[0]
    const error = { message: 'Network Error' }
    try {
      await errorHandler.rejected(error)
    } catch (e: any) {
      expect(e.message).toBe('Network Error')
    }
  })
})
