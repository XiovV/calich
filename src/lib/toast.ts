import { Toast } from "@base-ui/react/toast";

export const toastManager = Toast.createToastManager();

export const toast = {
  error(message: string) {
    toastManager.add({ title: message, type: "error" });
  },
};
