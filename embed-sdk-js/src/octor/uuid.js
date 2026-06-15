export default function () {
    // Use crypto.getRandomValues() for cryptographically secure random bytes
    // instead of Math.random() which is predictable and exploitable.
    const bytes = new Uint8Array(16);
    (typeof crypto !== 'undefined' && crypto.getRandomValues
        ? crypto
        : require('crypto').webcrypto
    ).getRandomValues(bytes);

    // Set version (4) and variant (RFC 4122) bits
    bytes[6] = (bytes[6] & 0x0f) | 0x40; // version 4
    bytes[8] = (bytes[8] & 0x3f) | 0x80; // variant 10

    const hex = Array.from(bytes, b => b.toString(16).padStart(2, '0')).join('');
    return (
        hex.slice(0, 8) + '-' +
        hex.slice(8, 12) + '-' +
        hex.slice(12, 16) + '-' +
        hex.slice(16, 20) + '-' +
        hex.slice(20, 32)
    );
}
