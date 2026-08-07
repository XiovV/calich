import { useEffect } from "react";
import { BrowserRouter, Navigate, Route, Routes } from "react-router";
import { AppShell } from "./components/layout/AppShell";
import { LoginPage } from "./auth/LoginPage";
import { ProtectedRoute } from "./auth/ProtectedRoute";
import { SettingsPage } from "./settings/SettingsPage";
import { SETTINGS_SECTIONS } from "./settings/settingsSections";
import { Toaster } from "./components/ui/Toaster";
import { useAuthStore } from "./lib/authStore";

function App() {
  const bootstrap = useAuthStore((state) => state.bootstrap);

  useEffect(() => {
    bootstrap();
  }, [bootstrap]);

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route
          path="/"
          element={
            <ProtectedRoute>
              <AppShell />
            </ProtectedRoute>
          }
        />
        <Route
          path="/settings"
          element={
            <ProtectedRoute>
              <SettingsPage />
            </ProtectedRoute>
          }
        >
          <Route index element={<Navigate to={SETTINGS_SECTIONS[0].path} replace />} />
          {SETTINGS_SECTIONS.map((section) => (
            <Route key={section.path} path={section.path} element={section.element} />
          ))}
        </Route>
      </Routes>
      <Toaster />
    </BrowserRouter>
  );
}

export default App;
