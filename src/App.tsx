import { useEffect } from "react";
import { BrowserRouter, Navigate, Route, Routes } from "react-router";
import { AppShell } from "./components/layout/AppShell";
import { AcceptWorkspaceInvitePage } from "./auth/AcceptWorkspaceInvitePage";
import { LoginPage } from "./auth/LoginPage";
import { RegisterPage } from "./auth/RegisterPage";
import { ProtectedRoute } from "./auth/ProtectedRoute";
import { PublicBookingPage } from "./booking/PublicBookingPage";
import { PublicIndexPage } from "./booking/PublicIndexPage";
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
        {/* The public Booking Link page (#324, ADR-0084, ADR-0087): a
            stranger with no Session opens /:handle/:slug — sibling to the
            routes above, deliberately outside ProtectedRoute below. A
            claimed Handle can never collide with a literal route (the
            reserved-word registry the backend enforces at claim time), and
            React Router itself ranks a static segment above a dynamic one at
            the same depth, so "/settings/account" et al. are never shadowed. */}
        <Route path="/:handle/:slug" element={<PublicBookingPage />} />
        {/* The public index page (#325, ADR-0084, ADR-0087): the derived
            rendering of an owner's Public Booking Links at /:handle, one
            path segment shallower than the route above — React Router
            ranks it as a distinct, less-specific match, so the two never
            shadow each other. */}
        <Route path="/:handle" element={<PublicIndexPage />} />
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
