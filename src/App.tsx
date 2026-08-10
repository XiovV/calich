import { useEffect } from "react";
import { BrowserRouter, Navigate, Route, Routes } from "react-router";
import { AppShell } from "./components/layout/AppShell";
import { AcceptWorkspaceInvitePage } from "./auth/AcceptWorkspaceInvitePage";
import { LoginPage } from "./auth/LoginPage";
import { RegisterPage } from "./auth/RegisterPage";
import { ProtectedRoute } from "./auth/ProtectedRoute";
import { SettingsModal } from "./settings/SettingsModal";
import { getSettingsSections } from "./settings/settingsSections";
import { Toaster } from "./components/ui/Toaster";
import { useAuthStore } from "./lib/authStore";

function App() {
  const bootstrap = useAuthStore((state) => state.bootstrap);
  const settingsSections = getSettingsSections();

  useEffect(() => {
    bootstrap();
  }, [bootstrap]);

  return (
    <BrowserRouter>
      <Routes>
        <Route path="/login" element={<LoginPage />} />
        <Route path="/register" element={<RegisterPage />} />
        <Route path="/accept-workspace-invite" element={<AcceptWorkspaceInvitePage />} />
        <Route
          path="/"
          element={
            <ProtectedRoute>
              <AppShell />
            </ProtectedRoute>
          }
        >
          <Route path="settings" element={<SettingsModal />}>
            <Route index element={<Navigate to={settingsSections[0].path} replace />} />
            {settingsSections.map((section) => (
              <Route key={section.path} path={section.path} element={section.element} />
            ))}
          </Route>
        </Route>
      </Routes>
      <Toaster />
    </BrowserRouter>
  );
}

export default App;
