import { useEffect } from "react";
import { BrowserRouter, Navigate, Route, Routes } from "react-router";
import { AppShell } from "./components/layout/AppShell";
import { LoginPage } from "./auth/LoginPage";
import { ProtectedRoute } from "./auth/ProtectedRoute";
import { SettingsPage } from "./settings/SettingsPage";
import { getSettingsSections } from "./settings/settingsSections";
import { Toaster } from "./components/ui/Toaster";
import { useAuthStore } from "./lib/authStore";

function App() {
  const bootstrap = useAuthStore((state) => state.bootstrap);
  const isAdmin = useAuthStore((state) => state.user?.isAdmin ?? false);
  const settingsSections = getSettingsSections(isAdmin);

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
          <Route index element={<Navigate to={settingsSections[0].path} replace />} />
          {settingsSections.map((section) => (
            <Route key={section.path} path={section.path} element={section.element} />
          ))}
        </Route>
      </Routes>
      <Toaster />
    </BrowserRouter>
  );
}

export default App;
