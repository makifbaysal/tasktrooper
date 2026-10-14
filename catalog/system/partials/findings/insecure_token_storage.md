---
key: partial.findings/insecure_token_storage
version: 1
---
### Tokens or passwords are kept in plain storage

Why it matters: localStorage, AsyncStorage, UserDefaults and SharedPreferences are readable by injected scripts, backups or other code on the device.

How to fix: Web: prefer httpOnly, Secure, SameSite cookies, or keep the token in memory. iOS: Keychain. Android: EncryptedSharedPreferences or the Keystore. React Native: react-native-keychain or expo-secure-store. Flutter: flutter_secure_storage. Migrate existing stored values on next launch.
