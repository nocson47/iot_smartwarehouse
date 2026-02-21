'use client';

import { useAuth } from '@/hooks/useAuth';
import { AuthForm } from '@/components/AuthForm';
import { Dashboard } from '@/components/Dashboard';

export default function Home() {
  const { isAuthenticated, isLoading, email, login, register, logout } = useAuth();

  if (isLoading) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-gray-100">
        <div className="text-xl text-gray-600">Loading...</div>
      </div>
    );
  }

  return (
    <div className="min-h-screen bg-gray-100 p-4">
      {isAuthenticated ? (
        <Dashboard email={email || ''} onLogout={logout} />
      ) : (
        <AuthForm onLogin={login} onRegister={register} />
      )}
    </div>
  );
}
