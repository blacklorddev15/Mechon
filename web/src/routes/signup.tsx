import { ArrowRight, Loader2 } from 'lucide-react'
import { type FormEvent, useState } from 'react'
import { Link, Navigate, useNavigate } from 'react-router'
import { SlatField } from '@/components/backdrop/slat-field'
import { Logo } from '@/components/brand/logo'
import { Reveal } from '@/components/motion/reveal'
import { ThemeToggle } from '@/components/shell/theme-toggle'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { useQuery } from '@tanstack/react-query'
import { ApiError, api } from '@/lib/api'
import { useMe, useSignup } from '@/lib/auth'

export function SignupPage() {
  const me = useMe()
  const site = useQuery({ queryKey: ['public-settings'], queryFn: api.publicSettings, staleTime: 5 * 60_000 })
  const brand = site.data?.brandName || 'Mechon'
  const signup = useSignup()
  const navigate = useNavigate()
  const [name, setName] = useState('')
  const [email, setEmail] = useState('')
  const [password, setPassword] = useState('')

  if (me.data) return <Navigate to="/" replace />

  const error = signup.error instanceof ApiError ? signup.error : signup.error ? new ApiError(0, 'unknown', 'Something went wrong.') : null

  function submit(e: FormEvent) {
    e.preventDefault()
    signup.mutate({ email, name, password }, { onSuccess: () => navigate('/', { replace: true }) })
  }

  return (
    <div className="relative flex min-h-svh flex-col overflow-hidden">
      <div className="absolute inset-0 [mask-image:radial-gradient(ellipse_80%_70%_at_50%_45%,black_35%,transparent_85%)]">
        <SlatField />
      </div>

      <header className="relative flex items-center justify-between px-5 py-5 sm:px-8">
        {site.data?.brandName ? <span className="font-display text-[19px] font-bold tracking-[-0.02em]">{site.data.brandName}</span> : <Logo />}
        <ThemeToggle />
      </header>

      <main className="relative flex flex-1 items-center justify-center px-4 pb-16">
        <Reveal y={18} className="w-full max-w-[440px]">
          <div className="glass rounded-[30px] px-6 py-9 sm:px-10 sm:py-11">
            <h1 className="text-display text-[clamp(2.2rem,1.9rem+1.4vw,2.7rem)]">Create your account.</h1>
            <p className="mt-3 text-[15.5px] text-muted-foreground">
              Start with a trial on {brand}. You can deploy a bot as soon as it is active.
            </p>

            <form onSubmit={submit} className="mt-8 space-y-5" noValidate>
              <div className="space-y-2">
                <Label htmlFor="name" className="text-[13.5px] font-semibold">
                  Name
                </Label>
                <Input id="name" autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} placeholder="Ada Lovelace" />
              </div>

              <div className="space-y-2">
                <Label htmlFor="email" className="text-[13.5px] font-semibold">
                  Email
                </Label>
                <Input
                  id="email"
                  type="email"
                  autoComplete="username"
                  required
                  value={email}
                  onChange={(e) => setEmail(e.target.value)}
                  placeholder="you@example.com"
                  aria-invalid={error?.code === 'email_taken' || undefined}
                />
              </div>

              <div className="space-y-2">
                <Label htmlFor="password" className="text-[13.5px] font-semibold">
                  Password
                </Label>
                <Input
                  id="password"
                  type="password"
                  autoComplete="new-password"
                  required
                  minLength={10}
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  placeholder="At least 10 characters"
                />
              </div>

              {error && (
                <p role="alert" className="text-[13px] font-medium text-destructive">
                  {error.message}
                </p>
              )}

              <Button type="submit" className="w-full" disabled={signup.isPending}>
                {signup.isPending ? <Loader2 className="size-4 animate-spin" /> : <ArrowRight className="size-4" />}
                Create account
              </Button>
            </form>

            <p className="mt-6 text-center text-[13px] text-faint-foreground">
              Already have an account?{' '}
              <Link to="/login" className="font-semibold text-muted-foreground hover:text-foreground">
                Sign in
              </Link>
            </p>
          </div>
        </Reveal>
      </main>
    </div>
  )
}
